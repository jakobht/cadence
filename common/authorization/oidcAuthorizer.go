package authorization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jmespath/go-jmespath"
	"go.uber.org/yarpc"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/config"
	"github.com/uber/cadence/common/dynamicconfig/dynamicproperties"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/common/types"
)

// oidcMode is the per-domain runtime mode of the OIDC authorizer.
type oidcMode int

const (
	modeEnabled oidcMode = iota
	modeShadow
	modeDisabled
)

const (
	oidcModeEnabledStr  = "enabled"
	oidcModeShadowStr   = "shadow"
	oidcModeDisabledStr = "disabled"

	defaultOIDCDiscoveryTimeoutSec = 10

	// oidcRolePrefix is the prefix on Keycloak (or any OIDC provider) realm-role names
	// that the OIDC authorizer interprets. A role matching `{prefix}{permission}` grants
	// that permission on any domain; `{prefix}{permission}/{domain}` scopes it to one
	// domain. Using '/' as the segment separator avoids ambiguity with domain names
	// containing dashes.
	oidcRolePrefix = "cadence/"
	// oidcRoleSeparator splits the permission segment from the optional domain segment.
	oidcRoleSeparator = "/"
)

func parseOIDCMode(s string) oidcMode {
	switch s {
	case oidcModeDisabledStr:
		return modeDisabled
	case oidcModeShadowStr:
		return modeShadow
	default:
		// Unknown values fail closed: treat as enabled so requests are still authorized.
		return modeEnabled
	}
}

// rolePermissions is the structured view of a token's `cadence-*` roles, used to answer
// `allows(permission, domain)` without iterating roles per request.
type rolePermissions struct {
	// isAdmin is true when the token has the literal role `cadence-admin`. Admin grants
	// every permission on every domain — domain-scoped admin (`cadence-admin-<domain>`)
	// is meaningless and treated as global admin too.
	isAdmin bool
	// scoped[perm] is the set of domain names for which this token has the permission.
	// An empty string in the set means "wildcard — any domain" (from a bare
	// `cadence-{perm}` role with no domain suffix).
	scoped map[Permission]map[string]struct{}
}

// parseRolePermissions turns a list of token roles into a permission lookup. Roles
// that don't begin with the OIDC role prefix are ignored. Roles with an unrecognized
// permission name (`cadence-foo`) are ignored.
func parseRolePermissions(roles []string) *rolePermissions {
	rp := &rolePermissions{scoped: map[Permission]map[string]struct{}{}}
	for _, role := range roles {
		rest, ok := strings.CutPrefix(role, oidcRolePrefix)
		if !ok || rest == "" {
			continue
		}
		permStr, domain, _ := strings.Cut(rest, oidcRoleSeparator)
		perm := NewPermission(permStr)
		if perm < 0 {
			continue
		}
		if perm == PermissionAdmin {
			rp.isAdmin = true
			continue
		}
		set, ok := rp.scoped[perm]
		if !ok {
			set = map[string]struct{}{}
			rp.scoped[perm] = set
		}
		set[domain] = struct{}{}
	}
	return rp
}

// allows reports whether the token may perform `permission` on `domain`.
// An empty domain (e.g. for non-domain APIs) matches a wildcard role only.
func (rp *rolePermissions) allows(permission Permission, domain string) bool {
	if rp.isAdmin {
		return true
	}
	set, ok := rp.scoped[permission]
	if !ok {
		return false
	}
	if _, wildcard := set[""]; wildcard {
		return true
	}
	_, ok = set[domain]
	return ok
}

type oidcAuthority struct {
	cfg    config.OIDCAuthorizer
	logger log.Logger

	// verifier is built once at construction and is safe for concurrent use.
	verifier *oidc.IDTokenVerifier

	domainModeFn dynamicproperties.StringPropertyFnWithDomainFilter
	adminModeFn  dynamicproperties.StringPropertyFn
}

// NewOIDCAuthorizer creates an Authorizer that verifies inbound bearer tokens against an
// OpenID Connect provider (e.g. Keycloak). The provider's discovery document is fetched at
// construction; the JWKS is then fetched lazily and rotated in the background by the
// underlying go-oidc RemoteKeySet. If discovery fails at startup, the constructor returns
// an error and the server fails to boot — fix the issuer URL or wait for the provider to
// come up before restarting Cadence.
//
// Authorization is role-name-driven: roles in the token of the form
// `cadence/{read|write|process|admin}[/{domain}]` grant the named permission on the named
// domain (or all domains, if no domain suffix is present). The admin claim, if present
// and true, grants all permissions on all domains.
//
// domainAuthMode / adminAuthMode return the current per-domain ("disabled"/"shadow"/"enabled")
// mode for a non-admin call and the global mode for admin calls, respectively. Callers wire
// these from a dynamicconfig.Collection — typically dc.GetStringPropertyFilteredByDomain
// (dynamicproperties.EnableAuthorizationV2) and dc.GetStringProperty(EnableAdminAuthorization).
func NewOIDCAuthorizer(
	cfg config.OIDCAuthorizer,
	logger log.Logger,
	domainAuthMode dynamicproperties.StringPropertyFnWithDomainFilter,
	adminAuthMode dynamicproperties.StringPropertyFn,
) (Authorizer, error) {
	if domainAuthMode == nil || adminAuthMode == nil {
		return nil, errors.New("OIDCAuthorizer requires non-nil domainAuthMode and adminAuthMode functions")
	}

	timeout := time.Duration(cfg.DiscoveryTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultOIDCDiscoveryTimeoutSec * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{
		ClientID:             cfg.ClientID,
		SupportedSigningAlgs: []string{oidc.RS256},
	})

	return &oidcAuthority{
		cfg:          cfg,
		logger:       logger,
		verifier:     verifier,
		domainModeFn: domainAuthMode,
		adminModeFn:  adminAuthMode,
	}, nil
}

// Authorize implements the Authorizer interface. It verifies the inbound token, applies
// the configured per-domain rollout mode (disabled / shadow / enabled), and authorizes
// based on the token's `cadence-*` roles.
func (a *oidcAuthority) Authorize(ctx context.Context, attrs *Attributes) (Result, error) {
	mode := a.modeFor(attrs.Permission, attrs.DomainName)
	if mode == modeDisabled {
		return Result{Decision: DecisionAllow}, nil
	}

	// Worker-stickiness shortcut. PollForActivityTask / PollForDecisionTask are the only
	// APIs whose wrapper populates attrs.TaskList, and we let the call through when the
	// task list is non-normal (sticky / ephemeral). The reason is purely operational:
	// current SDK clients do not attach tokens to poll calls, so without this shortcut
	// every existing worker breaks the moment OIDC is enabled for its domain.
	//
	// SECURITY TRADE-OFF: a caller able to reach the frontend can issue an unauthenticated
	// poll with kind=Sticky and (if they know or can guess the sticky list name, which is
	// server-generated and randomized) receive tasks from it. Closing this requires the
	// SDKs to attach tokens on every poll — a client-side change that's incompatible with
	// every existing worker binary. Until that lands, harden the frontend at the network
	// layer if this matters for your threat model.
	if attrs.TaskList != nil && attrs.TaskList.GetKind() != types.TaskListKindNormal {
		return Result{Decision: DecisionAllow}, nil
	}

	claims, authErr := a.verifyAndExtract(ctx)
	if authErr == nil && claims.Admin {
		return Result{Decision: DecisionAllow}, nil
	}
	if authErr == nil {
		rp := parseRolePermissions(claims.GetGroups())
		if !rp.allows(attrs.Permission, attrs.DomainName) {
			authErr = fmt.Errorf("token has no role granting %v on domain %q (roles=%v)", attrs.Permission, attrs.DomainName, claims.GetGroups())
		}
	}
	if authErr == nil {
		return Result{Decision: DecisionAllow}, nil
	}

	if mode == modeShadow {
		a.logger.Warn("OIDC authorize would have denied (shadow mode)",
			tag.WorkflowDomainName(attrs.DomainName),
			tag.HandlerCall(attrs.APIName),
			tag.Error(authErr))
		return Result{Decision: DecisionAllow}, nil
	}
	a.logger.Info("OIDC authorize denied",
		tag.WorkflowDomainName(attrs.DomainName),
		tag.HandlerCall(attrs.APIName),
		tag.Error(authErr))
	return Result{Decision: DecisionDeny}, nil
}

func (a *oidcAuthority) modeFor(perm Permission, domain string) oidcMode {
	if perm == PermissionAdmin {
		return parseOIDCMode(a.adminModeFn())
	}
	return parseOIDCMode(a.domainModeFn(domain))
}

// AuthConfig implements AuthConfigProvider so clients can discover the OIDC
// settings via GetClusterInfo. The returned ClientID is the same one configured
// for token-audience verification — in single-client setups (the common case),
// browser and CLI flows are both expected to authenticate against it. Operators
// running per-flow clients should configure their OIDC provider so all clients
// share audience.
func (a *oidcAuthority) AuthConfig() *types.AuthConfig {
	return &types.AuthConfig{
		Type: types.AuthTypeOIDC,
		OIDC: &types.OIDCAuthConfig{
			IssuerURL: a.cfg.IssuerURL,
			ClientID:  a.cfg.ClientID,
		},
	}
}

// verifyAndExtract runs the token-level pipeline: header extraction, signature/audience/
// issuer/expiry verification via go-oidc, our MaxJwtTTL ceiling, and JMESPath claim
// extraction. A non-nil error from here means the request failed authentication and is
// eligible for shadow-mode allow.
func (a *oidcAuthority) verifyAndExtract(ctx context.Context) (*JWTClaims, error) {
	token := yarpc.CallFromContext(ctx).Header(common.AuthorizationTokenHeaderName)
	if token == "" {
		return nil, errors.New("authorization header is empty")
	}
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("token verification: %w", err)
	}
	if err := a.validateExtraTTL(idToken); err != nil {
		return nil, err
	}
	return a.extractClaims(idToken)
}

// validateExtraTTL enforces the operator-configured ceiling on token lifetime — go-oidc
// already rejects expired tokens, but we also reject tokens whose remaining lifetime
// exceeds MaxJwtTTL. This prevents a stolen long-lived token from being usable indefinitely.
func (a *oidcAuthority) validateExtraTTL(idToken *oidc.IDToken) error {
	if timeLeft := time.Until(idToken.Expiry); timeLeft > time.Duration(a.cfg.MaxJwtTTL)*time.Second {
		return fmt.Errorf("token TTL %ds exceeds configured maximum %ds", int64(timeLeft.Seconds()), a.cfg.MaxJwtTTL)
	}
	return nil
}

// extractClaims pulls the configured groups + admin claims out of the verified token
// via JMESPath and populates a JWTClaims value.
func (a *oidcAuthority) extractClaims(idToken *oidc.IDToken) (*JWTClaims, error) {
	raw := map[string]interface{}{}
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("decoding token claims: %w", err)
	}

	out := &JWTClaims{}

	if a.cfg.GroupsAttributePath != "" {
		groups, err := jmespath.Search(a.cfg.GroupsAttributePath, raw)
		if err != nil {
			return nil, fmt.Errorf("extracting groups claim: %w", err)
		}
		gs, ok := groups.(string)
		if !ok {
			return nil, fmt.Errorf("groups claim resolved to %T, expected string (use a JMESPath expression like `realm_access.roles | join(' ', @)` to flatten arrays)", groups)
		}
		out.Groups = gs
	}

	if a.cfg.AdminAttributePath != "" {
		v, err := jmespath.Search(a.cfg.AdminAttributePath, raw)
		if err != nil {
			return nil, fmt.Errorf("extracting admin claim: %w", err)
		}
		if v != nil {
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("admin claim resolved to %T, expected bool", v)
			}
			out.Admin = b
		}
	}

	return out, nil
}
