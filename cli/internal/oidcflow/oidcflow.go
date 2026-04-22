// Package oidcflow implements an interactive OIDC authorization code flow
// with PKCE for acquiring ID tokens from an OIDC provider.
//
// It opens a browser for user authentication, handles the callback via a
// local HTTP server, and exchanges the authorization code for an ID token.
// This is the same flow used by Sigstore for keyless signing, implemented
// without depending on github.com/sigstore/sigstore.
package oidcflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

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
		Handler:           callbackHandler(state, codeCh, errCh),
	}
	go func() {
		if sErr := srv.Serve(listener); sErr != nil && !errors.Is(sErr, http.ErrServerClosed) {
			errCh <- sErr
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

func callbackHandler(expectedState string, codeCh chan<- string, errCh chan<- error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
		if r.FormValue("state") != expectedState {
			// Non-blocking: discard if channel already has an error (e.g. browser retry).
			select {
			case errCh <- errors.New("invalid state parameter in callback"):
			default:
			}
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		// Reject IdP error callbacks (e.g. access_denied, server_error).
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
		// Non-blocking: discard duplicate callbacks (browser retry, prefetch, etc.).
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

// successHTML is the page shown in the browser after successful OIDC authentication.
// This is a 1:1 copy of the Sigstore interactive success page from
// github.com/sigstore/sigstore/pkg/oauth to avoid confusing users with
// unexpected branding during a Sigstore OIDC flow.
const successHTML = `<!DOCTYPE html>
<html>
	<head>
		<title>Sigstore Authentication</title>
		<link id="favicon" rel="icon" type="image/svg"/>
		<style>
			:root { font-family: "Trebuchet MS", sans-serif; height: 100%; color: #444444; overflow: hidden; }
			body { display: flex; justify-content: center; height: 100%; margin: 0 10%; background: #FFEAD7; }
			.container { display: flex; flex-direction: column; justify-content: space-between; }
			.sigstore { color: #2F2E71; font-weight: bold; }
			.header { position: absolute; top: 30px; left: 22px; }
			.title { font-size: 3.5em; margin-bottom: 30px; animation: 750ms ease-in-out 0s 1 show; }
			.content { font-size: 1.5em; animation: 250ms hide, 750ms ease-in-out 250ms 1 show; }
			.anchor { position: relative; }
			.links { display: flex; justify-content: space-between; font-size: 1.2em; padding: 60px 0; position: absolute; bottom: 0; left: 0; right: 0; animation: 500ms hide, 750ms ease-in-out 500ms 1 show; }
			.link { color: #444444; text-decoration: none; user-select: none; }
			.link:hover { color: #6349FF; }
			.link:hover>.arrow { transform: scaleX(1.5) translateX(3px); }
			.link:hover>.sigstore { color: inherit; }
			.link, .arrow { transition: 200ms; }
			.arrow { display: inline-block; margin-left: 6px; transform: scaleX(1.5); }
			@keyframes hide { 0%, 100% { opacity: 0; } }
			@keyframes show { 0% { opacity: 0; transform: translateY(40px); } 100% { opacity: 1; } }
		</style>
	</head>
	<body>
		<div class="container">
			<div>
				<a class="header" href="https://sigstore.dev">
					<svg id="logo" xmlns="http://www.w3.org/2000/svg" xml:space="preserve" width="28.14" height="30.3">
						<circle r="7" cx="14" cy="15" fill="#FFEAD7"></circle>
						<path fill="#2F2E71" d="M27.8 10.9c-.3-1.2-.9-2.2-1.7-3.1-.6-.7-1.3-1.3-2-2-.7-.6-1.2-1.3-1.5-2.1-.2-.4-.4-.8-.7-1.2-.5-.7-1.3-1.2-2.1-1.6-1.3-.7-2.7-.9-4.2-.9-.8 0-1.6.1-2.4.3-1.2.2-2.3.7-3.4 1.3-.7.4-1.3.9-1.9 1.4-1 .8-2 1.6-2.8 2.6-.6.8-1.4 1.3-2.2 1.8-.8.4-1.4 1-2 1.6-.6.6-.9 1.3-.9 2.1 0 .6.1 1.2.2 1.7.2.9.6 1.7.9 2.6.2.5.3 1 .3 1.5s0 1-.1 1.5c-.1 1.1 0 2.3.2 3.4.2 1 .8 1.8 1.8 2.2.1.1.3.1.4.1.2.1.2.2.1.3l-.1.1c-.4.5-.7 1.1-.6 1.8.1 1.1 1.3 1.8 2.3 1.3.6-.2 1.2 0 1.4.4.1.1.1.2.2.3.2.5.4.9.7 1.3.4.5.9.7 1.6.6.4-.1.8-.2 1.2-.4.7-.4 1.3-.9 2-1.5.2-.2.4-.2.7-.2.4 0 .8.2 1.2.5.6.4 1.2.7 1.9.9 1.3.4 2.5.5 3.8.2 1.3-.3 2.4-.9 3.4-1.6.7-.5 1.2-1 1.6-1.7.4-.7.6-1.4.8-2.2.3-1.1.4-2.2.4-3.4.1-1 .2-1.9.5-2.8.2-.7.5-1.4.8-2.1.2-.6.4-1.2.5-1.9.1-1.1 0-2.1-.3-3.1zM14.9.8c.3-.1.7-.1 1-.1h.3c1.1 0 2.1.2 3.1.5.6.2 1.2.6 1.7 1s.7.9.9 1.4v.1c0 .1 0 .2-.1.2s-.1 0-.2-.1c-.4-.4-.7-.8-1.1-1.1-.6-.5-1.2-.9-2-1.1-1.1-.3-2.1-.5-3.2-.7h-.6c.1 0 .1 0 .2-.1-.1 0 0 0 0 0zm-4.5 12.4c.6 0 1.1.5 1.2 1.2 0 .6-.5 1.2-1.2 1.2-.6 0-1.2-.5-1.1-1.2 0-.7.5-1.2 1.1-1.2zm3.8 1.3v-3.4c0-2.3 2-3.1 3.6-2.5.3.1.6.3.9.5.2.2.2.5.1.8-.2.2-.4.3-.7.1-.2-.1-.5-.2-.7-.3-.6-.2-1.3 0-1.6.4-.1.2-.2.4-.2.7-.1.5 0 .9 0 1.4v5.9c0 1.2-.6 2.1-1.8 2.4-1 .3-1.9.2-2.7-.6-.2-.2-.3-.5-.1-.7.1-.2.4-.3.7-.2.3.1.6.3.9.4 1 .1 1.7-.3 1.7-1.4-.1-1.2-.1-2.3-.1-3.5zm-8.8 7.6h-.1c-.1-.1-.2-.1-.3-.2-.2-.2-.4-.3-.6-.5-.3-.3-.5-.6-.7-1-.4-.8-.8-1.7-1-2.7-.1-.5-.2-1-.2-1.5s-.1-1-.2-1.4c-.1-.7-.2-1.5-.2-2.2 0-.9.1-1.7.4-2.5.3-.9.7-1.7 1.4-2.4.6-.6 1.1-1.2 1.7-1.8.1-.1.3-.2.4-.2 0 .1-.1.3-.2.4-.3.4-.6.7-.9 1.1-.5.6-.9 1.2-1.2 1.8-.4.7-.7 1.4-.9 2.2-.1.4-.2.8-.2 1.2 0 .4-.1.8 0 1.3 0 .6.1 1.1.2 1.6.1.6.2 1.1.2 1.7 0 .7.2 1.4.4 2.1 0 .2.2.3.2.5.3.6.6 1.1 1.1 1.5.2.2.4.5.6.7 0 0 0 .1.1.1v.2zM8 24.6c-.4 0-.7.1-1.1.2-.4.1-.6-.1-.7-.5 0-.1-.1-.3 0-.4.1-.3.3-.3.5-.1.2.2.5.4.7.5.1.1.2.1.4.1.1 0 .2.1.4.2H8zm7.6 2.1c-.3.2-.7.3-1.1.3-.3 0-.6-.1-.9-.1h-.2c-.4.1-.7.1-1.1.2-.1 0-.3 0-.4.1H11c-.4 0-.7-.2-1-.5-.1-.1-.2-.3-.3-.5-.1-.1-.1-.2-.1-.4 0-.1.1-.1.2-.1h.1c.5.3 1.1.4 1.6.5.7.1 1.4.2 2.1.2.4 0 .7.1 1.1.1h.8c.2.1.1.1.1.2zm3.7-2.5c-.7.4-1.5.7-2.3.9-.2 0-.5.1-.7.1-.2 0-.5 0-.7.1-.4.1-.8 0-1.2 0-.3 0-.6-.1-.9 0h-.2c-.4-.1-.9-.2-1.3-.3-.5-.1-1-.3-1.4-.5-.4-.1-.8-.3-1.1-.5-.2-.1-.4-.3-.6-.4-.6-.6-1.2-1.1-1.7-1.6-.4-.5-.8-.9-1.2-1.4-.4-.6-.7-1.2-1-1.9l-.3-.9c-.1-.3-.2-.5-.2-.8v-.8c.3.8.5 1.7.9 2.5.7 1.6 1.7 3 3 4.1 1.4 1.1 2.9 1.8 4.6 2.1.9.2 1.8.2 2.7.2 1.1-.1 2.2-.3 3.2-.8.2-.1.3-.2.5-.2 0 .1 0 .1-.1.1zm.1-8.7c-.6 0-1.1-.5-1.1-1.2 0-.6.5-1.2 1.2-1.2.6 0 1.1.5 1.1 1.2s-.5 1.3-1.2 1.2zm6.2 5.7c0 .4-.1.8-.2 1.2-.1.4-.1.9-.3 1.3-.1.4-.2.7-.4 1.1-.1.3-.3.6-.6.8-.3.2-.5.4-.9.5-.4.2-.7.3-1.2.3h-.9c-.2-.1-.2-.1-.1-.3.1-.2.3-.3.5-.4.3-.2.6-.5.8-.7.7-.7 1.3-1.6 1.9-2.4.4-.4.6-1 .9-1.5.1-.1.1-.2.2-.3.3.2.3.3.3.4zm-15-16.8c1.7-.8 3.5-1.1 5.3-.9.4 0 .8.1 1.1.3l1.8.6c.6.2 1.2.5 1.7.8.7.4 1.3.9 1.9 1.5.8.8 1.5 1.6 2 2.6.3.6.5 1.2.7 1.8.2.7.4 1.5.4 2.2v.9c0 .4-.1.8-.1 1.2v-1c0-1.2-.3-2.3-.6-3.4l-.6-1.5c-.2-.6-.5-1.1-.9-1.6-.1-.1-.3-.1-.4-.2-.1 0-.1 0-.2-.1-.5-.5-1.1-1-1.7-1.5-.8-.6-1.7-1.1-2.6-1.4-.4-.2-.8-.3-1.2-.4-.9-.2-1.8-.4-2.7-.3h-.9c-.3 0-.6.1-1 .2-.6.1-1.2.3-1.7.5h-.1s-.1 0 0-.1c0 0 0-.1-.1-.2m16.2 11.1c-.1-.8 0-1.7 0-2.5-.1-.8-.2-1.6-.4-2.4.5.7.6 1.6.7 2.4 0 .8 0 1.7-.3 2.5zm.6.5c0-.3.1-.7.2-1.1.1-.4.1-.9.1-1.3v-.4c0-.8-.2-1.6-.4-2.4-.4-.9-.8-1.6-1.4-2.4-.5-.6-.9-1.2-1.4-1.8l-.2-.2c.1 0 .1 0 .1.1 1 .8 1.8 1.6 2.4 2.7.5 1 .9 2 1 3.1.3 1.3.1 2.5-.4 3.7z"/>
					</svg><svg xmlns="http://www.w3.org/2000/svg" xml:space="preserve" width="120" height="30.3" viewBox="28.14 0 120 30.3">
						<path fill="#2F2E71" d="M57.7 18c.9 0 1.9-.1 2.9.3.9.3 1.5.9 1.7 2 .1 1-.2 1.9-1.1 2.5-1.1.8-2.3.9-3.6 1-1.4 0-2.9 0-4.3-.6-1.6-.7-1.8-2.6-.4-3.6.3-.2.2-.3.1-.5-.7-.8-.7-2.2.3-2.8.3-.2.2-.3 0-.6-1.4-1.6-.7-4.1 1.3-4.8 1.6-.6 3.2-.6 4.8-.1.2.1.3.1.5-.1.3-.3.6-.5.9-.7.5-.3 1.1-.3 1.4 0 .3.4.3.9-.2 1.3-.7.5-.8 1-.6 1.9.4 1.5-.6 2.9-2.1 3.4-1.3.5-2.7.5-4 .2-.3-.1-.5-.1-.6.2-.1.3-.1.6.2.8.2.1.5.2.8.2h2zm-.6-2.7c.3 0 .5 0 .7-.1.8-.2 1.3-.7 1.4-1.4.1-.7-.3-1.3-.9-1.6-.8-.3-1.6-.3-2.4 0-1 .4-1.2 1.7-.6 2.4.5.6 1.2.7 1.8.7zm-.2 4.6h-1.8c-.4 0-.6.2-.7.5-.3.7.1 1.2.9 1.4 1.2.2 2.5.2 3.7-.1l.6-.3c.5-.3.4-1.1-.1-1.3-.2-.1-.5-.2-.7-.2h-1.9zm58.6-3.3h-3.2c-.3 0-.3.1-.3.4.2 1.2 1.3 2.1 2.8 2.3 1.1.1 2.1-.1 2.9-.9.2-.2.4-.3.6-.4.4-.2.8-.2 1.1.1.4.3.4.7.3 1.1-.3.9-.9 1.4-1.7 1.8-2.3 1-4.5.9-6.5-.6-1-.7-1.5-1.8-1.7-3-.3-1.7-.2-3.3.8-4.7 1.3-1.8 3.1-2.4 5.2-2.1 2 .3 3.4 1.3 4 3.2.2.6.3 1.2.2 1.8-.1.8-.4 1.1-1.2 1.1-1.1-.1-2.2-.1-3.3-.1zm-.7-1.9h2.4c.4 0 .4-.1.4-.5-.3-1.1-1.2-1.9-2.5-2-1.5-.1-2.6.6-3.1 1.9-.2.5-.1.6.4.6h2.4zm-23 6.7c-3.3 0-5.5-2.2-5.6-5.5 0-3.2 2.2-5.6 5.4-5.6 3.4 0 5.7 2.2 5.8 5.5 0 3.4-2.2 5.6-5.6 5.6zm0-2.1c1.9 0 3.2-1.3 3.3-3.3.1-2-1.3-3.4-3.2-3.4-1.8 0-3.2 1.4-3.2 3.3-.1 1.9 1.2 3.4 3.1 3.4zm-22.6 2.1c-1.1 0-2.4-.3-3.5-1.4-.3-.4-.6-.8-.7-1.3 0-.4.1-.7.4-.9.3-.2.7-.2 1 0 .3.2.6.4.8.6.9.9 2 1.1 3.2.9.6-.1 1-.5 1-1 .1-.5-.2-1-.8-1.2-.7-.3-1.4-.3-2.1-.5-.8-.2-1.6-.4-2.2-.9-1.5-1.2-1.4-3.5.2-4.6 1.2-.8 2.6-.9 4-.7 1 .1 1.9.5 2.6 1.2.3.3.4.6.5.9.1.4 0 .7-.3 1-.3.3-.7.3-1 .1-.4-.2-.7-.5-1.1-.8-.8-.6-1.7-.8-2.7-.5-.6.1-.9.5-.9 1s.3.9.8 1.1c.9.3 1.9.4 2.9.7.6.2 1.1.4 1.5.8 1.5 1.4 1.1 4.1-.9 5.1-.7.3-1.5.4-2.7.4zm-30.3 0c-1.5 0-3-.3-4.1-1.5-.3-.3-.4-.6-.5-1-.1-.4-.1-.8.3-1.1.4-.2.9-.2 1.3.1.3.2.6.5.8.7.9.8 1.9.9 3 .7.6-.1.9-.5.9-1.1 0-.5-.3-1-.8-1.2l-2.7-.6c-1.1-.3-2-.8-2.4-2-.6-1.7.4-3.4 2.2-3.9 1.7-.5 3.3-.3 4.8.5.6.3 1 .8 1.2 1.4.1.4.1.9-.3 1.1-.4.3-.8.2-1.2-.1-.4-.3-.7-.7-1.2-.9-.8-.4-1.5-.5-2.4-.3-.5.1-.8.4-.8.9s.2.8.7 1c.7.3 1.4.3 2.1.5.8.2 1.5.3 2.2.8 1.2.8 1.5 2.5.9 3.8-.7 1.4-1.9 1.8-3.3 2-.3.2-.5.2-.7.2zM78 15.8v-2.6c0-.3-.1-.4-.4-.4h-1.3c-.6-.1-.9-.5-.9-1s.4-1 .9-1h1.3c.3 0 .4-.1.4-.4V8.9c0-.7.5-1.1 1.1-1.2.6 0 1.1.4 1.2 1v.6c0 .4-.2 1 .1 1.3.3.3.9.1 1.3.1h1.6c.4 0 .7.3.8.8.1.4-.1.8-.4 1.1-.3.2-.6.2-.9.2h-2.1c-.3 0-.4 0-.4.4v4.7c0 1.1.9 1.6 1.9 1.1.3-.1.5-.3.7-.5.4-.3.8-.3 1.2 0 .4.3.4.8.2 1.2-.3.7-.9 1.1-1.5 1.3-1.3.5-2.6.5-3.8-.4-.7-.6-1-1.4-1.1-2.4.1-.7.1-1.6.1-2.4zm24.7-4.1c.8-1 1.8-1.4 3-1.3.7.1 1.4.3 1.9.9.3.4.5.9.4 1.5-.1.4-.3.7-.7.9-.5.2-.9 0-1.3-.3-.7-.7-1.3-.9-2.1-.5-.5.3-.8.7-1 1.3-.2.5-.3 1.1-.3 1.6v4.5c0 .5-.2.9-.6 1.1-.4.2-.8.2-1.2-.1-.4-.3-.5-.7-.5-1.1v-8.4c0-.7.4-1.2 1-1.3.7-.1 1.1.3 1.3 1.1.1-.1.1 0 .1.1zm-54 4.2v4.3c0 .8-.6 1.3-1.4 1.2-.5-.1-.9-.5-.9-1.1v-8.7c0-.7.5-1.1 1.2-1.1s1.1.4 1.2 1.2c-.1 1.3-.1 2.8-.1 4.2zm.3-8.2c0 .8-.6 1.4-1.4 1.4-.8 0-1.5-.6-1.5-1.4 0-.8.7-1.4 1.5-1.4s1.4.6 1.4 1.4z"/>
					</svg>
				</a>
			</div>
			<div>
				<div class="title">
					<span class="sigstore">sigstore </span>
					<span>authentication successful!</span>
				</div>
				<div class="content">
					<span>You may now close this page.</span>
				</div>
			</div>
			<div class="anchor">
				<div class="links">
					<a href="https://sigstore.dev/" class="link login"><span class="sigstore">sigstore</span> home <span class="arrow">&#x2192;</span></a>
					<a href="https://docs.sigstore.dev/" class="link login"><span class="sigstore">sigstore</span> documentation <span class="arrow">&#x2192;</span></a>
					<a href="https://blog.sigstore.dev/" class="link"><span class="sigstore">sigstore</span> blog <span class="arrow">&#x2192;</span></a>
				</div>
			</div>
		</div>
		<script>
			document.getElementById("favicon").setAttribute("href", "data:image/svg+xml," + encodeURIComponent(document.getElementById("logo").outerHTML));
		</script>
	</body>
</html>
`

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
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL)
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
