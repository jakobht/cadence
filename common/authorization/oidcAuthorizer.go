package authorization

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jmespath/go-jmespath"
	"go.uber.org/yarpc"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/cache"
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

type oidcAuthority struct {
	cfg         config.OIDCAuthorizer
	domainCache cache.DomainCache
	logger      log.Logger

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
// domainAuthMode / adminAuthMode return the current per-domain ("disabled"/"shadow"/"enabled")
// mode for a non-admin call and the global mode for admin calls, respectively. Callers wire
// these from a dynamicconfig.Collection — typically dc.GetStringPropertyFilteredByDomain
// (dynamicproperties.EnableAuthorizationV2) and dc.GetStringProperty(EnableAdminAuthorization).
func NewOIDCAuthorizer(
	cfg config.OIDCAuthorizer,
	logger log.Logger,
	domainCache cache.DomainCache,
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
		domainCache:  domainCache,
		logger:       logger,
		verifier:     verifier,
		domainModeFn: domainAuthMode,
		adminModeFn:  adminAuthMode,
	}, nil
}

// Authorize implements the Authorizer interface. It verifies the inbound token, applies
// the configured per-domain rollout mode (disabled / shadow / enabled), and delegates
// permission checks to the shared validatePermission helper.
func (a *oidcAuthority) Authorize(ctx context.Context, attrs *Attributes) (Result, error) {
	mode := a.modeFor(attrs.Permission, attrs.DomainName)
	if mode == modeDisabled {
		return Result{Decision: DecisionAllow}, nil
	}

	// Worker stickiness: cadence workers poll task lists with dynamically-generated
	// names (sticky/ephemeral kinds) for which there is no domain-level ACL to check
	// against, and in the common deployment shape workers don't carry user tokens at
	// all. Bypassing auth for these polls is what makes worker stickiness compatible
	// with per-domain auth being enabled on the rest of the API surface.
	if attrs.TaskList != nil && attrs.TaskList.GetKind() != types.TaskListKindNormal {
		return Result{Decision: DecisionAllow}, nil
	}

	claims, authErr := a.verifyAndExtract(ctx)
	if authErr == nil && claims.Admin {
		return Result{Decision: DecisionAllow}, nil
	}
	if authErr == nil {
		// Permission check needs domain ACL data. Domain cache failures are infra
		// errors and propagate as-is (never shadow-allowed).
		domain, err := a.domainCache.GetDomain(attrs.DomainName)
		if err != nil {
			a.logger.Info("OIDC authorize infra error", tag.Error(err))
			return Result{Decision: DecisionDeny}, err
		}
		authErr = validatePermission(claims, attrs, domain.GetInfo().Data)
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
// via JMESPath and populates a JWTClaims value that validatePermission can consume.
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
