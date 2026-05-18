package authorization

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/yarpc/api/encoding"
	"go.uber.org/yarpc/api/transport"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/dynamicconfig"
	"github.com/uber/cadence/common/dynamicconfig/dynamicproperties"
	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/types"
)

const (
	testIssuerClientID = "cadence-server"
	testDomainName     = "test-domain"
)

func TestParseRolePermissions(t *testing.T) {
	tests := []struct {
		name        string
		roles       []string
		wantAdmin   bool
		allowChecks []struct {
			perm   Permission
			domain string
			want   bool
		}
	}{
		{
			name:      "no cadence roles",
			roles:     []string{"unrelated", "another"},
			wantAdmin: false,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionRead, "any", false},
			},
		},
		{
			name:      "wildcard read",
			roles:     []string{"cadence/read"},
			wantAdmin: false,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionRead, "anything", true},
				{PermissionWrite, "anything", false},
			},
		},
		{
			name:      "scoped write on domain with dashes",
			roles:     []string{"cadence/write/alice-domain"},
			wantAdmin: false,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionWrite, "alice-domain", true},
				{PermissionWrite, "bob-domain", false},
				{PermissionRead, "alice-domain", false},
			},
		},
		{
			name:      "admin role grants everything",
			roles:     []string{"cadence/admin"},
			wantAdmin: true,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionRead, "x", true},
				{PermissionWrite, "y", true},
				{PermissionAdmin, "", true},
			},
		},
		{
			name:      "wildcard + scoped combine",
			roles:     []string{"cadence/read", "cadence/write/alice-domain"},
			wantAdmin: false,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionRead, "bob-domain", true},
				{PermissionWrite, "alice-domain", true},
				{PermissionWrite, "bob-domain", false},
			},
		},
		{
			name:      "unknown permission name ignored",
			roles:     []string{"cadence/frobnicate"},
			wantAdmin: false,
			allowChecks: []struct {
				perm   Permission
				domain string
				want   bool
			}{
				{PermissionRead, "any", false},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rp := parseRolePermissions(tc.roles)
			assert.Equal(t, tc.wantAdmin, rp.isAdmin)
			for _, c := range tc.allowChecks {
				assert.Equalf(t, c.want, rp.allows(c.perm, c.domain),
					"allows(%v, %q)", c.perm, c.domain)
			}
		})
	}
}

// oidcTestEnv bundles a fake OIDC provider + signing key + in-memory dynamic config so
// each test stands on its own.
type oidcTestEnv struct {
	server  *httptest.Server
	signer  jose.Signer
	dcStore dynamicconfig.Client
}

func newOIDCTestEnv(t *testing.T) *oidcTestEnv {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signingKey := jose.SigningKey{Algorithm: jose.RS256, Key: priv}
	signer, err := jose.NewSigner(signingKey, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-kid"))
	require.NoError(t, err)
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       priv.Public(),
		KeyID:     "test-kid",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}}
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                serverURL,
			"jwks_uri":                              serverURL + "/jwks",
			"authorization_endpoint":                serverURL + "/auth",
			"token_endpoint":                        serverURL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return &oidcTestEnv{server: srv, signer: signer, dcStore: dynamicconfig.NewInMemoryClient()}
}

func (e *oidcTestEnv) issuerURL() string { return e.server.URL }

// signToken builds a signed JWT with sane defaults; pass roles to populate
// realm_access.roles; pass mutate to tweak any other claim.
func (e *oidcTestEnv) signToken(t *testing.T, roles []string, mutate func(map[string]interface{})) string {
	t.Helper()
	now := time.Now()
	claims := map[string]interface{}{
		"iss":          e.issuerURL(),
		"aud":          testIssuerClientID,
		"sub":          "alice",
		"iat":          now.Unix(),
		"exp":          now.Add(5 * time.Minute).Unix(),
		"realm_access": map[string]interface{}{"roles": roles},
	}
	if mutate != nil {
		mutate(claims)
	}
	tok, err := jwt.Signed(e.signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return tok
}

func (e *oidcTestEnv) modeFns(t *testing.T) (dynamicproperties.StringPropertyFnWithDomainFilter, dynamicproperties.StringPropertyFn) {
	c := dynamicconfig.NewCollection(e.dcStore, testlogger.New(t))
	return c.GetStringPropertyFilteredByDomain(dynamicproperties.EnableAuthorizationV2),
		c.GetStringProperty(dynamicproperties.EnableAdminAuthorization)
}

func (e *oidcTestEnv) defaultConfig() config.OIDCAuthorizer {
	return config.OIDCAuthorizer{
		Enable:              true,
		IssuerURL:           e.issuerURL(),
		ClientID:            testIssuerClientID,
		GroupsAttributePath: "realm_access.roles | join(' ', @)",
		AdminAttributePath:  "cadence_admin",
		MaxJwtTTL:           3600,
	}
}

func ctxWithToken(t *testing.T, token string) context.Context {
	t.Helper()
	ctx := context.Background()
	ctx, call := encoding.NewInboundCall(ctx)
	require.NoError(t, call.ReadFromRequest(&transport.Request{
		Caller:    "test-caller",
		Service:   "cadence-frontend",
		Procedure: "test",
		Headers:   transport.NewHeaders().With(common.AuthorizationTokenHeaderName, token),
	}))
	return ctx
}

func TestOIDCAuthorizer_NewSucceedsWithDiscovery(t *testing.T) {
	env := newOIDCTestEnv(t)
	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), domainFn, adminFn)
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestOIDCAuthorizer_NewFailsWithoutModeFns(t *testing.T) {
	env := newOIDCTestEnv(t)
	domainFn, adminFn := env.modeFns(t)
	_, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), nil, adminFn)
	assert.ErrorContains(t, err, "non-nil domainAuthMode")
	_, err = NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), domainFn, nil)
	assert.ErrorContains(t, err, "non-nil domainAuthMode")
}

func TestOIDCAuthorizer_NewFailsOnDiscoveryError(t *testing.T) {
	env := newOIDCTestEnv(t)
	cfg := env.defaultConfig()
	cfg.IssuerURL = "http://127.0.0.1:1"
	cfg.DiscoveryTimeoutSeconds = 1
	domainFn, adminFn := env.modeFns(t)
	_, err := NewOIDCAuthorizer(cfg, testlogger.New(t), domainFn, adminFn)
	assert.Error(t, err)
}

func TestOIDCAuthorizer_Authorize(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, env *oidcTestEnv) (token string, attrs *Attributes, modes map[dynamicproperties.StringKey]string)
		decision Decision
	}{
		{
			name: "wildcard role grants read on any domain",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/read"}, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "domain-scoped role allows that domain",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/write/test-domain"}, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "domain-scoped role denies other domains",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/write/other-domain"}, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "admin role bypasses permission check",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/admin"}, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "admin claim bypasses permission check",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{}, func(c map[string]interface{}) { c["cadence_admin"] = true }),
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "no matching role denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/read"}, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "wrong audience denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/read"}, func(c map[string]interface{}) { c["aud"] = "someone-else" }),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "expired token denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/read"}, func(c map[string]interface{}) {
						c["exp"] = time.Now().Add(-time.Minute).Unix()
					}),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "TTL exceeds maximum denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return env.signToken(t, []string{"cadence/read"}, func(c map[string]interface{}) {
						c["exp"] = time.Now().Add(2 * time.Hour).Unix()
					}),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "shadow mode allows even with bad token",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return "not-a-real-token",
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeShadowStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "disabled mode allows without verifying",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return "", &Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeDisabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "non-normal task list bypasses verification",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				kind := types.TaskListKindSticky
				return "", &Attributes{
						DomainName: testDomainName,
						Permission: PermissionProcess,
						TaskList:   &types.TaskList{Name: "tl", Kind: &kind},
					},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "empty header denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, map[dynamicproperties.StringKey]string) {
				return "", &Attributes{DomainName: testDomainName, Permission: PermissionRead},
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newOIDCTestEnv(t)
			token, attrs, modeMap := tc.setup(t, env)
			for k, v := range modeMap {
				require.NoError(t, env.dcStore.UpdateValue(k, v))
			}
			domainFn, adminFn := env.modeFns(t)
			a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), domainFn, adminFn)
			require.NoError(t, err)
			res, err := a.Authorize(ctxWithToken(t, token), attrs)
			assert.NoError(t, err)
			assert.Equal(t, tc.decision, res.Decision)
		})
	}
}

func TestOIDCAuthorizer_AdminCallUsesAdminMode(t *testing.T) {
	env := newOIDCTestEnv(t)
	require.NoError(t, env.dcStore.UpdateValue(dynamicproperties.EnableAdminAuthorization, oidcModeDisabledStr))
	// Domain mode is "enabled" (default) — but since the call is admin, the admin DC wins.

	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), domainFn, adminFn)
	require.NoError(t, err)

	res, err := a.Authorize(ctxWithToken(t, ""), &Attributes{Permission: PermissionAdmin})
	assert.NoError(t, err)
	assert.Equal(t, DecisionAllow, res.Decision, "admin DC=disabled should allow")
}

func TestOIDCAuthorizer_GroupsClaimWrongType(t *testing.T) {
	env := newOIDCTestEnv(t)
	require.NoError(t, env.dcStore.UpdateValue(dynamicproperties.EnableAuthorizationV2, oidcModeEnabledStr))

	cfg := env.defaultConfig()
	cfg.GroupsAttributePath = "realm_access.roles" // returns []interface{}, not string
	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(cfg, testlogger.New(t), domainFn, adminFn)
	require.NoError(t, err)

	res, err := a.Authorize(
		ctxWithToken(t, env.signToken(t, []string{"cadence/read"}, nil)),
		&Attributes{DomainName: testDomainName, Permission: PermissionRead},
	)
	assert.NoError(t, err)
	assert.Equal(t, DecisionDeny, res.Decision)
}

func TestOIDCAuthorizer_AuthConfig(t *testing.T) {
	env := newOIDCTestEnv(t)
	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), domainFn, adminFn)
	require.NoError(t, err)

	provider, ok := a.(AuthConfigProvider)
	require.True(t, ok, "oidcAuthority must implement AuthConfigProvider")
	cfg := provider.AuthConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, types.AuthTypeOIDC, cfg.Type)
	require.NotNil(t, cfg.OIDC)
	assert.Equal(t, env.issuerURL(), cfg.OIDC.IssuerURL)
	assert.Equal(t, testIssuerClientID, cfg.OIDC.ClientID)
}

func modes(adminMode, domainMode string) map[dynamicproperties.StringKey]string {
	return map[dynamicproperties.StringKey]string{
		dynamicproperties.EnableAdminAuthorization: adminMode,
		dynamicproperties.EnableAuthorizationV2:    domainMode,
	}
}
