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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultipleAuthEnabled(t *testing.T) {
	cfg := Authorization{
		OAuthAuthorizer: OAuthAuthorizer{
			Enable: true,
		},
		NoopAuthorizer: NoopAuthorizer{
			Enable: true,
		},
	}

	err := cfg.Validate()
	assert.EqualError(t, err, "[AuthorizationConfig] More than one authorizer is enabled")
}

func TestTTLIsZero(t *testing.T) {
	cfg := Authorization{
		OAuthAuthorizer: OAuthAuthorizer{
			Enable:         true,
			JwtCredentials: &JwtCredentials{},
			MaxJwtTTL:      0,
		},
		NoopAuthorizer: NoopAuthorizer{
			Enable: false,
		},
	}

	err := cfg.Validate()
	assert.EqualError(t, err, "[OAuthConfig] MaxTTL must be greater than 0")
}

func TestPublicKeyIsEmpty(t *testing.T) {
	cfg := Authorization{
		OAuthAuthorizer: OAuthAuthorizer{
			Enable: true,
			JwtCredentials: &JwtCredentials{
				Algorithm: "",
				PublicKey: "",
			},
			MaxJwtTTL: 1000000,
		},
		NoopAuthorizer: NoopAuthorizer{
			Enable: false,
		},
	}

	err := cfg.Validate()
	assert.EqualError(t, err, "[OAuthConfig] PublicKey can't be empty")
}

func TestAlgorithmIsInvalid(t *testing.T) {
	cfg := Authorization{
		OAuthAuthorizer: OAuthAuthorizer{
			Enable: true,
			JwtCredentials: &JwtCredentials{
				Algorithm: "SHA256",
				PublicKey: "public",
			},
			MaxJwtTTL: 1000000,
		},
		NoopAuthorizer: NoopAuthorizer{
			Enable: false,
		},
	}

	err := cfg.Validate()
	assert.EqualError(t, err, "[OAuthConfig] The only supported Algorithm is RS256")
}

func TestCorrectValidation(t *testing.T) {
	cfg := Authorization{
		OAuthAuthorizer: OAuthAuthorizer{
			Enable: true,
			JwtCredentials: &JwtCredentials{
				Algorithm: "RS256",
				PublicKey: "public",
			},
			MaxJwtTTL: 1000000,
		},
		NoopAuthorizer: NoopAuthorizer{
			Enable: false,
		},
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestOIDCValidation(t *testing.T) {
	validBase := OIDCAuthorizer{
		Enable:              true,
		IssuerURL:           "https://kc.example.com/realms/cadence",
		ClientID:            "cadence-server",
		MaxJwtTTL:           3600,
		GroupsAttributePath: "realm_access.roles | join(' ', @)",
		AdminAttributePath:  "cadence_admin",
	}

	tests := []struct {
		name    string
		mutate  func(*OIDCAuthorizer)
		wantErr string // "" = expect success
	}{
		{name: "valid", mutate: func(*OIDCAuthorizer) {}, wantErr: ""},
		{name: "missing issuerURL", mutate: func(c *OIDCAuthorizer) { c.IssuerURL = "" }, wantErr: "issuerURL is required"},
		{name: "issuerURL without scheme", mutate: func(c *OIDCAuthorizer) { c.IssuerURL = "kc.example.com/realms/cadence" }, wantErr: "must be an http(s) URL"},
		{name: "issuerURL with wrong scheme", mutate: func(c *OIDCAuthorizer) { c.IssuerURL = "ftp://kc.example.com" }, wantErr: "must be an http(s) URL"},
		{name: "missing clientID", mutate: func(c *OIDCAuthorizer) { c.ClientID = "" }, wantErr: "clientID is required"},
		{name: "zero MaxJwtTTL", mutate: func(c *OIDCAuthorizer) { c.MaxJwtTTL = 0 }, wantErr: "maxJwtTTL must be greater than 0"},
		{name: "empty groups path allowed", mutate: func(c *OIDCAuthorizer) { c.GroupsAttributePath = "" }, wantErr: ""},
		{name: "invalid groups JMESPath", mutate: func(c *OIDCAuthorizer) { c.GroupsAttributePath = "realm_access.[bad" }, wantErr: "groupsAttributePath is not a valid JMESPath"},
		{name: "empty admin path allowed", mutate: func(c *OIDCAuthorizer) { c.AdminAttributePath = "" }, wantErr: ""},
		{name: "invalid admin JMESPath", mutate: func(c *OIDCAuthorizer) { c.AdminAttributePath = "[" }, wantErr: "adminAttributePath is not a valid JMESPath"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBase
			tc.mutate(&cfg)
			err := (&Authorization{OIDCAuthorizer: cfg}).Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
