package authorization

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/yarpc/api/encoding"
	"go.uber.org/yarpc/api/transport"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/cache"
	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/constants"
	"github.com/uber/cadence/common/dynamicconfig"
	"github.com/uber/cadence/common/dynamicconfig/dynamicproperties"
	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/types"
)

const (
	testIssuerClientID = "cadence-server"
	testDomainName     = "test-domain"
	testGroupName      = "cadence-read"
)

// oidcTestEnv bundles a fake OIDC provider, signing key, and dynamic config so each test
// can stand on its own without leaking state between cases.
type oidcTestEnv struct {
	server  *httptest.Server
	signer  jose.Signer
	keyID   string
	dcStore dynamicconfig.Client
}

func newOIDCTestEnv(t *testing.T) *oidcTestEnv {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyID := "test-kid"
	signingKey := jose.SigningKey{Algorithm: jose.RS256, Key: priv}
	signer, err := jose.NewSigner(signingKey, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID))
	require.NoError(t, err)

	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       priv.Public(),
		KeyID:     keyID,
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

	dc := dynamicconfig.NewInMemoryClient()
	return &oidcTestEnv{server: srv, signer: signer, keyID: keyID, dcStore: dc}
}

func (e *oidcTestEnv) issuerURL() string { return e.server.URL }

// signToken builds a signed JWT with the given claims overlaid on a sane default.
func (e *oidcTestEnv) signToken(t *testing.T, mutate func(c map[string]interface{})) string {
	t.Helper()
	now := time.Now()
	claims := map[string]interface{}{
		"iss": e.issuerURL(),
		"aud": testIssuerClientID,
		"sub": "alice",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"realm_access": map[string]interface{}{
			"roles": []string{testGroupName},
		},
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

// ctxWithToken attaches a yarpc inbound call carrying the cadence-authorization header.
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

// domainCacheWithGroups returns a cache mock that resolves testDomainName to a domain
// whose data grants read+write access to the supplied groups.
func domainCacheWithGroups(t *testing.T, ctrl *gomock.Controller, readGroup, writeGroup string) *cache.MockDomainCache {
	t.Helper()
	dc := cache.NewMockDomainCache(ctrl)
	entry := cache.NewLocalDomainCacheEntryForTest(
		&persistence.DomainInfo{
			Name: testDomainName,
			Data: map[string]string{
				constants.DomainDataKeyForReadGroups:  readGroup,
				constants.DomainDataKeyForWriteGroups: writeGroup,
			},
		},
		nil, "")
	dc.EXPECT().GetDomain(testDomainName).Return(entry, nil).AnyTimes()
	return dc
}

func TestOIDCAuthorizer_NewSucceedsWithDiscovery(t *testing.T) {
	env := newOIDCTestEnv(t)
	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), nil, domainFn, adminFn)
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestOIDCAuthorizer_NewFailsWithoutModeFns(t *testing.T) {
	env := newOIDCTestEnv(t)
	domainFn, adminFn := env.modeFns(t)
	_, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), nil, nil, adminFn)
	assert.ErrorContains(t, err, "non-nil domainAuthMode")
	_, err = NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), nil, domainFn, nil)
	assert.ErrorContains(t, err, "non-nil domainAuthMode")
}

func TestOIDCAuthorizer_NewFailsOnDiscoveryError(t *testing.T) {
	env := newOIDCTestEnv(t)
	cfg := env.defaultConfig()
	cfg.IssuerURL = "http://127.0.0.1:1" // unreachable port
	cfg.DiscoveryTimeoutSeconds = 1
	domainFn, adminFn := env.modeFns(t)
	_, err := NewOIDCAuthorizer(cfg, testlogger.New(t), nil, domainFn, adminFn)
	assert.Error(t, err)
}

func TestOIDCAuthorizer_Authorize(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, env *oidcTestEnv) (token string, attrs *Attributes, dc cache.DomainCache, modes map[dynamicproperties.StringKey]string)
		decision   Decision
		expectErr  bool
		modeAdmin  string
		modeDomain string
	}{
		{
			name: "valid token with matching group is allowed",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				ctrl := gomock.NewController(t)
				dc := domainCacheWithGroups(t, ctrl, testGroupName, "")
				return env.signToken(t, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					dc, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "admin claim short-circuits permission check",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				token := env.signToken(t, func(c map[string]interface{}) {
					c["cadence_admin"] = true
				})
				return token,
					&Attributes{DomainName: testDomainName, Permission: PermissionWrite},
					nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "wrong audience denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				token := env.signToken(t, func(c map[string]interface{}) {
					c["aud"] = "someone-else"
				})
				return token,
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "expired token denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				token := env.signToken(t, func(c map[string]interface{}) {
					c["exp"] = time.Now().Add(-time.Minute).Unix()
				})
				return token,
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "TTL exceeds maximum denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				token := env.signToken(t, func(c map[string]interface{}) {
					c["exp"] = time.Now().Add(2 * time.Hour).Unix()
				})
				return token,
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "wrong group denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				ctrl := gomock.NewController(t)
				dc := domainCacheWithGroups(t, ctrl, "other-group", "")
				return env.signToken(t, nil),
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					dc, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "shadow mode allows even with bad token",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				return "not-a-real-token",
					&Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeShadowStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "disabled mode allows without verifying",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				return "", &Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeDisabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "empty domain with no token denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				return "", &Attributes{Permission: PermissionRead}, nil,
					modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
		{
			name: "non-normal task list bypasses verification",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				kind := types.TaskListKindSticky
				return "", &Attributes{
					DomainName: testDomainName,
					Permission: PermissionProcess,
					TaskList:   &types.TaskList{Name: "tl", Kind: &kind},
				}, nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionAllow,
		},
		{
			name: "empty header denies",
			setup: func(t *testing.T, env *oidcTestEnv) (string, *Attributes, cache.DomainCache, map[dynamicproperties.StringKey]string) {
				return "", &Attributes{DomainName: testDomainName, Permission: PermissionRead},
					nil, modes(oidcModeEnabledStr, oidcModeEnabledStr)
			},
			decision: DecisionDeny,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newOIDCTestEnv(t)
			token, attrs, dc, modeMap := tc.setup(t, env)
			for k, v := range modeMap {
				require.NoError(t, env.dcStore.UpdateValue(k, v))
			}
			domainFn, adminFn := env.modeFns(t)
			a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), dc, domainFn, adminFn)
			require.NoError(t, err)

			res, err := a.Authorize(ctxWithToken(t, token), attrs)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.decision, res.Decision)
		})
	}
}

func TestOIDCAuthorizer_DomainCacheErrorPropagates(t *testing.T) {
	env := newOIDCTestEnv(t)
	require.NoError(t, env.dcStore.UpdateValue(dynamicproperties.EnableAuthorizationV2, oidcModeEnabledStr))

	ctrl := gomock.NewController(t)
	dc := cache.NewMockDomainCache(ctrl)
	dc.EXPECT().GetDomain(testDomainName).Return(nil, errors.New("cassandra unavailable"))

	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), dc, domainFn, adminFn)
	require.NoError(t, err)

	res, err := a.Authorize(
		ctxWithToken(t, env.signToken(t, nil)),
		&Attributes{DomainName: testDomainName, Permission: PermissionRead},
	)
	assert.Error(t, err)
	assert.Equal(t, DecisionDeny, res.Decision)
}

func TestOIDCAuthorizer_AdminCallUsesAdminMode(t *testing.T) {
	env := newOIDCTestEnv(t)
	require.NoError(t, env.dcStore.UpdateValue(dynamicproperties.EnableAdminAuthorization, oidcModeDisabledStr))
	// Domain mode is "enabled" (default) — but since the call is admin, the admin DC wins.

	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(env.defaultConfig(), testlogger.New(t), nil, domainFn, adminFn)
	require.NoError(t, err)

	res, err := a.Authorize(ctxWithToken(t, ""), &Attributes{Permission: PermissionAdmin})
	assert.NoError(t, err)
	assert.Equal(t, DecisionAllow, res.Decision, "admin DC=disabled should allow")
}

func TestOIDCAuthorizer_GroupsClaimWrongType(t *testing.T) {
	env := newOIDCTestEnv(t)
	require.NoError(t, env.dcStore.UpdateValue(dynamicproperties.EnableAuthorizationV2, oidcModeEnabledStr))

	ctrl := gomock.NewController(t)
	dc := domainCacheWithGroups(t, ctrl, testGroupName, "")
	cfg := env.defaultConfig()
	cfg.GroupsAttributePath = "realm_access.roles" // returns []interface{}, not string
	domainFn, adminFn := env.modeFns(t)
	a, err := NewOIDCAuthorizer(cfg, testlogger.New(t), dc, domainFn, adminFn)
	require.NoError(t, err)

	res, err := a.Authorize(
		ctxWithToken(t, env.signToken(t, nil)),
		&Attributes{DomainName: testDomainName, Permission: PermissionRead},
	)
	assert.NoError(t, err)
	assert.Equal(t, DecisionDeny, res.Decision)
}

func modes(adminMode, domainMode string) map[dynamicproperties.StringKey]string {
	return map[dynamicproperties.StringKey]string{
		dynamicproperties.EnableAdminAuthorization: adminMode,
		dynamicproperties.EnableAuthorizationV2:    domainMode,
	}
}
