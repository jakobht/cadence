// Copyright (c) 2021 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package config

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmespath/go-jmespath"
)

type (
	Authorization struct {
		OAuthAuthorizer OAuthAuthorizer `yaml:"oauthAuthorizer"`
		OIDCAuthorizer  OIDCAuthorizer  `yaml:"oidcAuthorizer"`
		NoopAuthorizer  NoopAuthorizer  `yaml:"noopAuthorizer"`
	}

	NoopAuthorizer struct {
		Enable bool `yaml:"enable"`
	}

	OAuthAuthorizer struct {
		Enable bool `yaml:"enable"`
		// Max of TTL in the claim
		MaxJwtTTL int64 `yaml:"maxJwtTTL"`
		// Credentials to verify/create the JWT using public/private keys
		JwtCredentials *JwtCredentials `yaml:"jwtCredentials"`
		// Provider
		Provider *OAuthProvider `yaml:"provider"`
	}

	JwtCredentials struct {
		// support: RS256 (RSA using SHA256)
		Algorithm string `yaml:"algorithm"`
		// Public Key Path for verifying JWT token passed in from external clients
		PublicKey string `yaml:"publicKey"`
	}

	// OAuthProvider is used to validate tokens provided by 3rd party Identity Provider service
	OAuthProvider struct {
		JWKSURL             string `yaml:"jwksURL"`
		GroupsAttributePath string `yaml:"groupsAttributePath"`
		AdminAttributePath  string `yaml:"adminAttributePath"`
	}

	// OIDCAuthorizer validates ID tokens from a standards-compliant OpenID Connect provider
	// (e.g. Keycloak, Auth0, Okta). It performs full RFC-compliant verification: signature
	// against the JWKS published in the provider's discovery document, audience match against
	// ClientID, issuer match against IssuerURL, and expiry check.
	//
	// Per-domain authorization (group/admin claim mapping) is then applied in the same way as
	// the OAuthAuthorizer.
	OIDCAuthorizer struct {
		Enable bool `yaml:"enable"`
		// IssuerURL is the OIDC provider's issuer URL. Used for OIDC discovery
		// (the provider must serve <IssuerURL>/.well-known/openid-configuration).
		// Required.
		IssuerURL string `yaml:"issuerURL"`
		// ClientID is the expected audience (`aud` claim) for tokens. Required.
		ClientID string `yaml:"clientID"`
		// GroupsAttributePath is the JMESPath expression that extracts the user's groups
		// from the verified token claims as a space-separated string. Example for Keycloak:
		// `realm_access.roles | join(' ', @)`.
		GroupsAttributePath string `yaml:"groupsAttributePath"`
		// AdminAttributePath is the JMESPath expression that extracts a boolean admin
		// claim. If true, the token is granted full access without per-domain checks.
		AdminAttributePath string `yaml:"adminAttributePath"`
		// MaxJwtTTL is the maximum lifetime (in seconds) accepted for an inbound token.
		// Tokens whose `exp - now` exceeds this are rejected. Required, > 0.
		MaxJwtTTL int64 `yaml:"maxJwtTTL"`
		// DiscoveryTimeoutSeconds is the timeout for the initial OIDC discovery request
		// at server startup. Defaults to 10 if unset.
		DiscoveryTimeoutSeconds int `yaml:"discoveryTimeoutSeconds"`
	}
)

// Validate validates the persistence config
func (a *Authorization) Validate() error {
	enabled := 0
	if a.OAuthAuthorizer.Enable {
		enabled++
	}
	if a.OIDCAuthorizer.Enable {
		enabled++
	}
	if a.NoopAuthorizer.Enable {
		enabled++
	}
	if enabled > 1 {
		return fmt.Errorf("[AuthorizationConfig] More than one authorizer is enabled")
	}

	if a.OAuthAuthorizer.Enable {
		if err := a.validateOAuth(); err != nil {
			return err
		}
	}

	if a.OIDCAuthorizer.Enable {
		if err := a.validateOIDC(); err != nil {
			return err
		}
	}

	return nil
}

func (a *Authorization) validateOIDC() error {
	c := a.OIDCAuthorizer
	if c.IssuerURL == "" {
		return errors.New("[OIDCConfig] issuerURL is required")
	}
	u, err := url.Parse(c.IssuerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("[OIDCConfig] issuerURL must be an http(s) URL, got %q", c.IssuerURL)
	}
	if c.ClientID == "" {
		return errors.New("[OIDCConfig] clientID is required")
	}
	if c.MaxJwtTTL <= 0 {
		return errors.New("[OIDCConfig] maxJwtTTL must be greater than 0")
	}
	if c.GroupsAttributePath != "" {
		if _, err := jmespath.Compile(c.GroupsAttributePath); err != nil {
			return fmt.Errorf("[OIDCConfig] groupsAttributePath is not a valid JMESPath expression: %w", err)
		}
	}
	if c.AdminAttributePath != "" {
		if _, err := jmespath.Compile(c.AdminAttributePath); err != nil {
			return fmt.Errorf("[OIDCConfig] adminAttributePath is not a valid JMESPath expression: %w", err)
		}
	}
	return nil
}

func (a *Authorization) validateOAuth() error {
	oauthConfig := a.OAuthAuthorizer

	if oauthConfig.MaxJwtTTL <= 0 {
		return fmt.Errorf("[OAuthConfig] MaxTTL must be greater than 0")
	}

	if oauthConfig.JwtCredentials == nil && oauthConfig.Provider == nil {
		return errors.New("jwtCredentials or provider must be provided")
	}

	if oauthConfig.JwtCredentials != nil {
		if oauthConfig.JwtCredentials.PublicKey == "" {
			return fmt.Errorf("[OAuthConfig] PublicKey can't be empty")
		}

		if oauthConfig.JwtCredentials.Algorithm != jwt.SigningMethodRS256.Name {
			return fmt.Errorf("[OAuthConfig] The only supported Algorithm is RS256")
		}
	}

	if oauthConfig.Provider != nil {
		if oauthConfig.Provider.JWKSURL == "" {
			return fmt.Errorf("[OAuthConfig] JWKSURL is not set")
		}
	}

	return nil
}
