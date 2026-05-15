// Package oidcflow implements an interactive OIDC authorization code flow
// with PKCE for acquiring ID tokens from an OIDC provider.
//
// It opens a browser for user authentication, handles the callback via a
// local HTTP server, and exchanges the authorization code for an ID token.
// This is the same flow used by Sigstore for keyless signing, implemented
// without depending on github.com/sigstore/sigstore.
//
// # Security Model
//
// The flow uses PKCE S256 (RFC 7636) as the sole authorization code protection.
// This is appropriate because: (1) the OCM CLI is a public OAuth client that
// cannot hold a client secret, (2) the loopback redirect URI (127.0.0.1) limits
// code interception to same-machine processes which PKCE fully mitigates per
// RFC 8252 §7.1, (3) the acquired ID token is used immediately for a single
// signing operation and not persisted, removing the need for
// DPoP (Demonstrating Proof of Possession) or PAR (Pushed Authorization Requests (RFC 9126)).
//
// The flow does not send prompt=consent or prompt=login; the provider's default
// session behavior applies, giving users seamless re-authentication for repeated
// signing operations.
//
// RFC 9207 issuer identification is validated when the provider includes the iss
// parameter in the callback. Providers that omit it (including the public Sigstore
// instance) are accepted without iss verification.
package oidcflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	_ "embed"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

//go:embed assets/success.html
var successHTML string

const (
	DefaultIssuer   = "https://oauth2.sigstore.dev/auth"
	DefaultClientID = "sigstore"

	callbackPath    = "/auth/callback"
	callbackTimeout = 120 * time.Second
)

// Token holds the raw OIDC ID token string after a successful flow.
type Token struct {
	RawToken string
}

// Options configures the OIDC flow.
type Options struct {
	Issuer   string
	ClientID string
}

// GetIDToken performs an interactive OIDC authorization code flow with PKCE.
// It opens the user's browser for authentication and waits for the callback.
func GetIDToken(ctx context.Context, opts Options) (*Token, error) {
	if opts.Issuer == "" {
		opts.Issuer = DefaultIssuer
	}
	if opts.ClientID == "" {
		opts.ClientID = DefaultClientID
	}

	provider, err := oidc.NewProvider(ctx, opts.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider discovery: %w", err)
	}

	pkce, err := newPKCE(provider)
	if err != nil {
		return nil, err
	}

	state, err := randomString(32)
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	nonce, err := randomString(32)
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback listener: %w", err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", addr.Port, callbackPath)

	srv := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		Handler:           callbackHandler(state, opts.Issuer, codeCh, errCh),
	}
	go func() {
		if sErr := srv.Serve(listener); sErr != nil && !errors.Is(sErr, http.ErrServerClosed) {
			select {
			case errCh <- sErr:
			default:
			}
		}
	}()
	defer srv.Shutdown(ctx) //nolint:errcheck // best-effort shutdown

	config := oauth2.Config{
		ClientID:    opts.ClientID,
		Endpoint:    provider.Endpoint(),
		Scopes:      []string{oidc.ScopeOpenID, "email"},
		RedirectURL: redirectURL,
	}

	authOpts := append(pkce.authURLOpts(),
		oauth2.AccessTypeOnline,
		oidc.Nonce(nonce),
	)
	authURL := config.AuthCodeURL(state, authOpts...)

	if err := openBrowser(ctx, authURL, errCh); err != nil {
		return nil, fmt.Errorf("open browser: %w (URL: %s)", err, authURL)
	}

	code, err := waitForCode(ctx, codeCh, errCh)
	if err != nil {
		return nil, fmt.Errorf("receive auth callback: %w", err)
	}

	token, err := config.Exchange(ctx, code, pkce.tokenURLOpts()...)
	if err != nil {
		return nil, fmt.Errorf("exchange code for token: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("id_token not present in token response")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: config.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, errors.New("nonce mismatch in id token")
	}

	return &Token{RawToken: rawIDToken}, nil
}

func callbackHandler(expectedState, expectedIssuer string, codeCh chan<- string, errCh chan<- error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
		// Constant-time comparison prevents timing side-channel recovery of the state value.
		if subtle.ConstantTimeCompare([]byte(r.FormValue("state")), []byte(expectedState)) != 1 {
			select {
			case errCh <- errors.New("invalid state parameter in callback"):
			default:
			}
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		// RFC 9207: validate iss when present; permissive when absent.
		if iss := r.FormValue("iss"); iss != "" && iss != expectedIssuer {
			select {
			case errCh <- fmt.Errorf("issuer mismatch in callback: got %q, expected %q", iss, expectedIssuer):
			default:
			}
			http.Error(w, "issuer mismatch", http.StatusBadRequest)
			return
		}
		if idpErr := r.FormValue("error"); idpErr != "" {
			desc := r.FormValue("error_description")
			select {
			case errCh <- fmt.Errorf("identity provider error: %s (%s)", idpErr, desc):
			default:
			}
			http.Error(w, "authentication failed: "+idpErr, http.StatusForbidden)
			return
		}
		code := r.FormValue("code")
		if code == "" {
			select {
			case errCh <- errors.New("callback missing authorization code"):
			default:
			}
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		select {
		case codeCh <- code:
			fmt.Fprint(w, successHTML)
		default:
			http.Error(w, "callback already handled", http.StatusConflict)
		}
	})
	return mux
}

func waitForCode(ctx context.Context, codeCh <-chan string, errCh <-chan error) (string, error) {
	timer := time.NewTimer(callbackTimeout)
	defer timer.Stop()
	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", fmt.Errorf("authentication cancelled: %w", ctx.Err())
	case <-timer.C:
		return "", errors.New("timed out waiting for authentication callback")
	}
}

// pkce implements Proof Key for Code Exchange (RFC 7636).
type pkce struct {
	challenge string
	verifier  string
}

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

func (p *pkce) authURLOpts() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", p.challenge),
	}
}

func (p *pkce) tokenURLOpts() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", p.verifier),
	}
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(ctx context.Context, rawURL string, errCh chan<- error) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse auth URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("refusing to open non-HTTPS auth URL (scheme %q)", parsed.Scheme)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", rawURL)
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", rawURL)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", "\""+rawURL+"\"") //nolint:gosec // rawURL is validated as HTTPS above
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			select {
			case errCh <- fmt.Errorf("browser opener failed: %w", err):
			default:
			}
		}
	}()
	return nil
}
