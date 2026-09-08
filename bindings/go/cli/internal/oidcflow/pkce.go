package oidcflow

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// pkce holds a single PKCE S256 verifier/challenge pair bound to one OIDC
// authorization request. Instances are not reusable across flows.
//
// PKCE (Proof Key for Code Exchange, RFC 7636) is the sole authorization-code
// protection used by this package: a fresh, cryptographically random verifier
// is hashed (SHA-256) into a challenge that is sent on the authorize request;
// the verifier itself is presented on the token request, proving the same
// client started the flow. On a public client (no client secret) and a
// loopback redirect, this is the RFC 8252 §7.1 recommended construction.
//
// Only S256 is implemented. Providers that do not advertise S256 in
// code_challenge_methods_supported are rejected up-front by newPKCE.
type pkce struct {
	challenge string
	verifier  string
}

// newPKCE inspects the provider's discovery document, fails fast if S256 is
// not supported, and returns a fresh verifier/challenge pair.
func newPKCE(provider *oidc.Provider) (*pkce, error) {
	var claims struct {
		Methods []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse provider claims: %w", err)
	}

	supported := false
	for _, m := range claims.Methods {
		if m == "S256" {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("OIDC provider %s does not support PKCE S256", provider.Endpoint().AuthURL)
	}

	verifier, err := randomString(64)
	if err != nil {
		return nil, fmt.Errorf("generate PKCE verifier: %w", err)
	}

	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return &pkce{challenge: challenge, verifier: verifier}, nil
}

// authURLOpts returns the parameters added to the authorization request:
// code_challenge_method=S256 and the SHA-256 challenge.
func (p *pkce) authURLOpts() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", p.challenge),
	}
}

// tokenURLOpts returns the parameters added to the token exchange request:
// the original verifier, which the authorization server hashes and compares
// against the challenge it received earlier.
func (p *pkce) tokenURLOpts() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", p.verifier),
	}
}
