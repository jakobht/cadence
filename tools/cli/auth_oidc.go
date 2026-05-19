package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"

	"github.com/uber/cadence/client/frontend"
)

// Memoized so multiple newContext calls per command (workflow list opens
// several) don't each re-issue a device-authorization request and invalidate
// the previous one.
var (
	authOnce      sync.Once
	authOnceToken string
)

// Errors are logged inline; callers have no recourse — the only fallback is
// to run unauthenticated and let the server reject.
func ensureAuthTokenOnce(ctx context.Context, wf frontend.Client, profile string) string {
	authOnce.Do(func() {
		token, err := ensureAuthToken(ctx, wf, profile)
		authOnceToken = token
		if err != nil {
			fmt.Fprintf(os.Stderr, "OIDC sign-in failed: %v\n", err)
		}
	})
	return authOnceToken
}

// User-named profiles (via --profile=foo) are intentionally NOT considered
// when no --profile is given: e.g. `cadence workflow list` (with --profile) uses the
// auto-generated default identity for the cluster. Errors are non-fatal so
// unauth-only commands still work when not signed in.
func ensureAuthToken(ctx context.Context, wf frontend.Client, profile string) (string, error) {
	if profile != "" {
		if t, ok := tokenFromCachedProfile(ctx, profile); ok {
			return t, nil
		}
	}

	issuer, clientID, ok := serverOIDCConfig(ctx, wf)
	if !ok {
		return "", nil
	}
	if profile == "" {
		profile = autoProfile(issuer, clientID)
		if t, ok := tokenFromCachedProfile(ctx, profile); ok {
			return t, nil
		}
	}

	fresh, err := runDeviceAuthFlow(ctx, issuer, clientID, profile)
	if err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// serverOIDCConfig calls GetClusterInfo and extracts (issuer, client_id), or
// reports !ok if the server doesn't advertise OIDC (or can't be reached).
func serverOIDCConfig(ctx context.Context, wf frontend.Client) (issuer, clientID string, ok bool) {
	info, err := wf.GetClusterInfo(ctx)
	if err != nil || info.AuthConfig == nil || info.AuthConfig.OIDC == nil || info.AuthConfig.OIDC.IssuerURL == "" {
		return "", "", false
	}
	return info.AuthConfig.OIDC.IssuerURL, info.AuthConfig.OIDC.ClientID, true
}

func tokenFromCachedProfile(ctx context.Context, profile string) (string, bool) {
	entry := lookupEntryByProfile(profile)
	if entry == nil {
		return "", false
	}
	if !entry.expired() {
		return entry.AccessToken, true
	}
	if entry.RefreshToken == "" {
		return "", false
	}
	refreshed, err := refreshAccessToken(ctx, entry)
	if err != nil {
		return "", false
	}
	return refreshed.AccessToken, true
}

func refreshAccessToken(ctx context.Context, entry *authEntry) (*authEntry, error) {
	disc, err := fetchOIDCDiscovery(ctx, entry.IssuerURL)
	if err != nil {
		return nil, err
	}
	cfg := &oauth2.Config{
		ClientID: entry.ClientID,
		Endpoint: oauth2.Endpoint{TokenURL: disc.TokenEndpoint},
		Scopes:   []string{"openid", "email"},
	}
	src := cfg.TokenSource(ctx, &oauth2.Token{
		RefreshToken: entry.RefreshToken,
		Expiry:       entry.ExpiresAt,
	})
	tok, err := src.Token()
	if err != nil {
		return nil, err
	}
	entry.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		entry.RefreshToken = tok.RefreshToken
	}
	if !tok.Expiry.IsZero() {
		entry.ExpiresAt = tok.Expiry
	}
	if err := saveEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func runDeviceAuthFlow(ctx context.Context, issuer, clientID, profile string) (*authEntry, error) {
	disc, err := fetchOIDCDiscovery(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	if disc.DeviceAuthEndpoint == "" {
		return nil, fmt.Errorf("provider %s does not advertise device_authorization_endpoint", issuer)
	}

	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			TokenURL:      disc.TokenEndpoint,
			DeviceAuthURL: disc.DeviceAuthEndpoint,
		},
		Scopes: []string{"openid", "email"},
	}
	dResp, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	verify := dResp.VerificationURIComplete
	if verify == "" {
		verify = fmt.Sprintf("%s (enter code %s)", dResp.VerificationURI, dResp.UserCode)
	}
	fmt.Fprintf(os.Stderr, "Signing in. Visit:\n  %s\nWaiting for confirmation...\n", verify)

	tok, err := cfg.DeviceAccessToken(ctx, dResp)
	if err != nil {
		return nil, fmt.Errorf("polling for token: %w", err)
	}

	if profile == "" {
		profile = autoProfile(issuer, clientID)
	}
	entry := &authEntry{
		Profile:      profile,
		IssuerURL:    issuer,
		ClientID:     clientID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry,
	}
	if err := saveEntry(entry); err != nil {
		return nil, fmt.Errorf("persisting auth state: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Signed in (%s).\n", profile)
	return entry, nil
}

type oidcDiscovery struct {
	TokenEndpoint      string `json:"token_endpoint"`
	DeviceAuthEndpoint string `json:"device_authorization_endpoint"`
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

func fetchOIDCDiscovery(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decoding discovery: %w", err)
	}
	return &d, nil
}

func revokeRefreshToken(ctx context.Context, endSessionEndpoint, clientID, refreshToken string) error {
	form := url.Values{
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endSessionEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func decodeJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("base64-decoding payload: %w", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parsing payload JSON: %w", err)
	}
	return claims, nil
}
