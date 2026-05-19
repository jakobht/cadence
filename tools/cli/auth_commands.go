package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/uber/cadence/tools/common/commoncli"
)

func newAuthCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:   "login",
			Usage:  "Run the OIDC device flow against the connected cluster and cache the token. Use --profile to tag the entry.",
			Action: AuthLogin,
		},
		{
			Name:   "logout",
			Usage:  "Revoke the cached token at the OIDC provider and delete the local entry. Defaults to the connected cluster; pass --profile to act on a specific entry without a server round-trip.",
			Action: AuthLogout,
		},
		{
			Name:   "info",
			Usage:  "Show every cached identity (or just one if --profile is given).",
			Action: AuthInfo,
		},
	}
}

func AuthLogin(c *cli.Context) error {
	wf, err := getWorkflowClient(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context, 10*time.Minute)
	defer cancel()

	issuer, clientID, ok := serverOIDCConfig(ctx, wf)
	if !ok {
		return commoncli.Problem("login", errors.New("server does not advertise an OIDC config"))
	}
	profile := c.String(flagAuthProfile)
	if profile == "" {
		profile = autoProfile(issuer, clientID)
	}

	f, err := loadAuthFile()
	if err != nil {
		return commoncli.Problem("reading auth cache", err)
	}
	f.removeByProfile(profile)
	if err := saveAuthFile(f); err != nil {
		return commoncli.Problem("clearing existing entry", err)
	}

	entry, err := runDeviceAuthFlow(ctx, issuer, clientID, profile)
	if err != nil {
		return commoncli.Problem("login", err)
	}
	if claims, decErr := decodeJWTClaims(entry.AccessToken); decErr == nil {
		if user, _ := claims["preferred_username"].(string); user != "" {
			fmt.Fprintf(os.Stderr, "Signed in as %s.\n", user)
		}
	}
	return nil
}

func AuthLogout(c *cli.Context) error {
	if profile := c.String(flagAuthProfile); profile != "" {
		return authLogoutByProfile(c, profile)
	}
	return authLogoutCurrent(c)
}

func authLogoutByProfile(c *cli.Context, profile string) error {
	f, err := loadAuthFile()
	if err != nil {
		return commoncli.Problem("reading auth cache", err)
	}
	entry := f.lookupByProfile(profile)
	if entry == nil {
		fmt.Fprintf(os.Stderr, "No cached entry for profile %q.\n", profile)
		return nil
	}
	bestEffortRevoke(c.Context, entry)
	f.removeByProfile(profile)
	if err := saveAuthFile(f); err != nil {
		return commoncli.Problem("updating auth cache", err)
	}
	fmt.Fprintf(os.Stderr, "Signed out of %s.\n", profile)
	return nil
}

func authLogoutCurrent(c *cli.Context) error {
	wf, err := getWorkflowClient(c)
	if err != nil {
		return err
	}
	issuer, clientID, ok := serverOIDCConfig(c.Context, wf)
	if !ok {
		fmt.Fprintln(os.Stderr, "Server does not advertise OIDC; nothing to log out from.")
		return nil
	}
	return authLogoutByProfile(c, autoProfile(issuer, clientID))
}

func bestEffortRevoke(ctx context.Context, entry *authEntry) {
	if entry.RefreshToken == "" {
		return
	}
	disc, err := fetchOIDCDiscovery(ctx, entry.IssuerURL)
	if err != nil || disc.EndSessionEndpoint == "" {
		return
	}
	if err := revokeRefreshToken(ctx, disc.EndSessionEndpoint, entry.ClientID, entry.RefreshToken); err != nil {
		fmt.Fprintf(os.Stderr, "Server-side revocation failed (continuing with local cleanup): %v\n", err)
	}
}

// AuthInfo reads the cache directly; no GetClusterInfo call needed.
func AuthInfo(c *cli.Context) error {
	f, err := loadAuthFile()
	if err != nil {
		return commoncli.Problem("reading auth cache", err)
	}
	entries := f.Entries
	if profile := c.String(flagAuthProfile); profile != "" {
		entry := f.lookupByProfile(profile)
		if entry == nil {
			fmt.Fprintf(os.Stderr, "No cached entry for profile %q.\n", profile)
			return nil
		}
		entries = []*authEntry{entry}
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "Not signed in to any clusters.")
		return nil
	}
	for i, entry := range entries {
		if i > 0 {
			fmt.Println()
		}
		printAuthEntry(entry)
	}
	return nil
}

func printAuthEntry(entry *authEntry) {
	claims, _ := decodeJWTClaims(entry.AccessToken)
	user, _ := claims["preferred_username"].(string)
	if user == "" {
		user, _ = claims["sub"].(string)
	}
	refresh := "(no refresh token — next access expiry will re-prompt)"
	if entry.RefreshToken != "" {
		refresh = refreshTokenStatus(entry.RefreshToken)
	}
	fmt.Printf("Profile:        %s\n", entry.Profile)
	fmt.Printf("Issuer:         %s\n", entry.IssuerURL)
	fmt.Printf("Client:         %s\n", entry.ClientID)
	fmt.Printf("User:           %s\n", user)
	// Refresh first — it's what determines when the user gets re-prompted.
	fmt.Printf("Refresh until:  %s\n", refresh)
	fmt.Printf("Access until:   %s\n", formatRelativeTime(entry.ExpiresAt))
	if roles := rolesFromClaims(claims); len(roles) > 0 {
		fmt.Printf("Roles:          %s\n", strings.Join(roles, ", "))
	}
}

func rolesFromClaims(claims map[string]interface{}) []string {
	ra, ok := claims["realm_access"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := ra["roles"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func refreshTokenStatus(token string) string {
	claims, err := decodeJWTClaims(token)
	if err != nil {
		return "(opaque — not introspectable client-side)"
	}
	expF, ok := claims["exp"].(float64)
	if !ok {
		return "(no usable `exp` claim)"
	}
	return formatRelativeTime(time.Unix(int64(expF), 0))
}

func formatRelativeTime(t time.Time) string {
	d := time.Until(t).Round(time.Second)
	if d > 0 {
		return fmt.Sprintf("%s (in %s)", t.Format(time.RFC3339), d)
	}
	return fmt.Sprintf("%s (expired %s ago)", t.Format(time.RFC3339), -d)
}
