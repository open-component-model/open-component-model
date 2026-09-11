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

	callbackPath           = "/auth/callback"
	defaultCallbackTimeout = 120 * time.Second
)

// Token holds the raw OIDC ID token string after a successful flow.
type Token struct {
	RawToken string
}

// Options configures the OIDC flow.
type Options struct {
	Issuer   string
	ClientID string
	// CallbackTimeout bounds how long GetIDToken waits for the OIDC redirect
	// callback after the browser is opened. Zero applies the package default
	// (defaultCallbackTimeout).
	CallbackTimeout time.Duration
}

// GetIDToken performs an interactive OIDC authorization code flow with PKCE
// and returns the verified raw ID token. The flow is:
//  1. Discover the OIDC provider and generate PKCE/state/nonce.
//  2. Start a localhost HTTP server to receive the redirect callback.
//  3. Open the user's browser to the authorization URL.
//  4. Wait for the callback (bounded by callbackTimeout).
//  5. Exchange the code for tokens and verify the ID token (audience + nonce).
//
// See the package doc for the security model.
func GetIDToken(ctx context.Context, opts Options) (*Token, error) {
	opts.applyDefaults()

	provider, err := oidc.NewProvider(ctx, opts.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider discovery: %w", err)
	}
	pkce, err := newPKCE(provider)
	if err != nil {
		return nil, err
	}
	state, nonce, err := newStateAndNonce()
	if err != nil {
		return nil, err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback listener: %w", err)
	}
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, callbackPath)

	srv := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		Handler:           callbackHandler(state, opts.Issuer, codeCh, errCh),
	}
	go func() {
		if sErr := srv.Serve(listener); sErr != nil && !errors.Is(sErr, http.ErrServerClosed) {
			errCh <- sErr
		}
	}()
	defer func() { _ = srv.Shutdown(ctx) }()

	rawIDToken, err := runAuthFlow(ctx, provider, pkce, opts.ClientID, redirectURL, state, nonce, opts.CallbackTimeout, codeCh, errCh)
	if err != nil {
		return nil, err
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: opts.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, errors.New("nonce mismatch in id token")
	}
	return &Token{RawToken: rawIDToken}, nil
}

func (o *Options) applyDefaults() {
	if o.Issuer == "" {
		o.Issuer = DefaultIssuer
	}
	if o.ClientID == "" {
		o.ClientID = DefaultClientID
	}
	if o.CallbackTimeout == 0 {
		o.CallbackTimeout = defaultCallbackTimeout
	}
}

func newStateAndNonce() (state, nonce string, err error) {
	if state, err = randomString(32); err != nil {
		return "", "", fmt.Errorf("generate state: %w", err)
	}
	if nonce, err = randomString(32); err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}
	return state, nonce, nil
}

// runAuthFlow opens the browser, waits for the callback, and exchanges the
// authorization code for an ID token. The returned token is not yet verified.
func runAuthFlow(ctx context.Context, provider *oidc.Provider, pkce *pkce, clientID, redirectURL, state, nonce string, callbackTimeout time.Duration, codeCh <-chan string, errCh chan error) (string, error) {
	config := oauth2.Config{
		ClientID:    clientID,
		Endpoint:    provider.Endpoint(),
		Scopes:      []string{oidc.ScopeOpenID, "email"},
		RedirectURL: redirectURL,
	}
	authURL := config.AuthCodeURL(state, append(pkce.authURLOpts(), oauth2.AccessTypeOnline, oidc.Nonce(nonce))...)

	if err := openBrowser(ctx, authURL, errCh); err != nil {
		return "", fmt.Errorf("open browser: %w (URL: %s)", err, authURL)
	}

	code, err := waitForCode(ctx, callbackTimeout, codeCh, errCh)
	if err != nil {
		return "", fmt.Errorf("receive auth callback: %w", err)
	}

	token, err := config.Exchange(ctx, code, pkce.tokenURLOpts()...)
	if err != nil {
		return "", fmt.Errorf("exchange code for token: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", errors.New("id_token not present in token response")
	}
	return rawIDToken, nil
}

// callbackHandler terminates the OAuth redirect on the loopback listener:
// validates state (constant-time) and iss (RFC 9207, permissive when absent),
// then forwards the auth code on codeCh or an error on errCh. Channels are
// non-blocking; duplicate callbacks get 409.
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
			_, _ = fmt.Fprint(w, successHTML)
		default:
			http.Error(w, "callback already handled", http.StatusConflict)
		}
	})
	return mux
}

func waitForCode(ctx context.Context, callbackTimeout time.Duration, codeCh <-chan string, errCh <-chan error) (string, error) {
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
