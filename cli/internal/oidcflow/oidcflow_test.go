package oidcflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomString(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	s1, err := randomString(32)
	r.NoError(err)
	r.NotEmpty(s1)

	s2, err := randomString(32)
	r.NoError(err)
	r.NotEqual(s1, s2, "two random strings should differ")
}

func TestPKCEChallenge(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	verifier := "test-verifier-value"
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])

	p := &pkce{challenge: expected, verifier: verifier}

	authOpts := p.authURLOpts()
	r.Len(authOpts, 2)

	tokenOpts := p.tokenURLOpts()
	r.Len(tokenOpts, 1)
}

func TestCallbackHandler_ValidState(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=auth-code-123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)

	select {
	case code := <-codeCh:
		r.Equal("auth-code-123", code)
	default:
		t.Fatal("expected code on channel")
	}
}

func TestCallbackHandler_InvalidState(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("expected-state", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong-state&code=auth-code", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusBadRequest, rec.Code)

	select {
	case err := <-errCh:
		r.ErrorContains(err, "invalid state")
	default:
		t.Fatal("expected error on channel")
	}
}

func TestWaitForCode_Success(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	codeCh <- "test-code"

	code, err := waitForCode(context.Background(), codeCh, errCh)
	r.NoError(err)
	r.Equal("test-code", code)
}

func TestWaitForCode_Error(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	errCh <- errors.New("callback error")

	_, err := waitForCode(context.Background(), codeCh, errCh)
	r.ErrorContains(err, "callback error")
}

func TestWaitForCode_Timeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// Verify the timeout error message text via the errCh path (avoids waiting
	// for the real callbackTimeout duration in unit tests).
	codeCh := make(chan string)
	errCh := make(chan error, 1)
	errCh <- errors.New("timed out waiting for authentication callback")

	_, err := waitForCode(context.Background(), codeCh, errCh)
	r.ErrorContains(err, "timed out waiting for authentication callback")
}

func TestOptions_Defaults(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	r.NotEmpty(DefaultIssuer)
	r.NotEmpty(DefaultClientID)
}

func TestOptions_Custom(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	opts := Options{
		Issuer:   "https://custom.issuer.dev",
		ClientID: "custom-client",
	}
	r.Equal("https://custom.issuer.dev", opts.Issuer)
	r.Equal("custom-client", opts.ClientID)
}
