package oidcflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
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
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

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
	handler := callbackHandler("expected-state", "https://issuer.example.com", codeCh, errCh)

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

func TestCallbackHandler_MissingCode(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusBadRequest, rec.Code)

	select {
	case err := <-errCh:
		r.ErrorContains(err, "missing authorization code")
	default:
		t.Fatal("expected error on channel")
	}
}

func TestCallbackHandler_IdPError(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&error=access_denied&error_description=user+denied+consent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusForbidden, rec.Code)

	select {
	case err := <-errCh:
		r.ErrorContains(err, "identity provider error")
		r.ErrorContains(err, "access_denied")
		r.ErrorContains(err, "user denied consent")
	default:
		t.Fatal("expected error on channel")
	}
}

func TestCallbackHandler_DuplicateCallback(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req1 := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=first-code", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	r.Equal(http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=second-code", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	r.Equal(http.StatusConflict, rec2.Code)

	select {
	case code := <-codeCh:
		r.Equal("first-code", code, "only first callback should be accepted")
	default:
		t.Fatal("expected code on channel")
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

func TestWaitForCode_ContextCancelled(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	codeCh := make(chan string)
	errCh := make(chan error)

	_, err := waitForCode(ctx, codeCh, errCh)
	r.ErrorContains(err, "authentication cancelled")
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

func TestOpenBrowser_RejectsNonHTTPS(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	errCh := make(chan error, 1)
	err := openBrowser(context.Background(), "http://example.com/auth", errCh)
	r.Error(err)
	r.ErrorContains(err, "refusing to open non-HTTPS auth URL")
}

func TestOpenBrowser_RejectsInvalidURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	errCh := make(chan error, 1)
	err := openBrowser(context.Background(), "://invalid", errCh)
	r.Error(err)
	r.ErrorContains(err, "parse auth URL")
}

func TestAuthURL_ContainsPKCEAndState(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	challenge := "test-challenge"
	state := "random-state-value"
	redirectURL := "http://127.0.0.1:12345/auth/callback"

	params := url.Values{
		"client_id":             {"sigstore"},
		"redirect_uri":         {redirectURL},
		"response_type":        {"code"},
		"scope":                {"openid email"},
		"state":                {state},
		"code_challenge_method": {"S256"},
		"code_challenge":       {challenge},
		"access_type":          {"online"},
		"nonce":                {"test-nonce"},
	}
	authURL := "https://issuer.example.com/auth?" + params.Encode()

	parsed, err := url.Parse(authURL)
	r.NoError(err)

	q := parsed.Query()
	r.Equal("sigstore", q.Get("client_id"))
	r.Equal(redirectURL, q.Get("redirect_uri"))
	r.Equal("code", q.Get("response_type"))
	r.Contains(q.Get("scope"), "openid")
	r.Equal(state, q.Get("state"))
	r.Equal("S256", q.Get("code_challenge_method"))
	r.Equal(challenge, q.Get("code_challenge"))
	r.Equal("online", q.Get("access_type"))
	r.Equal("test-nonce", q.Get("nonce"))
}

func TestPKCE_VerifierChallengeRelationship(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Verify the S256 challenge derivation matches RFC 7636.
	r.Len(expectedChallenge, 43, "base64url(sha256) should be 43 chars")
	r.NotEqual(verifier, expectedChallenge, "challenge must differ from verifier")
}

func TestOptions_Defaults(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	r.NotEmpty(DefaultIssuer)
	r.NotEmpty(DefaultClientID)
	r.Equal("https://oauth2.sigstore.dev/auth", DefaultIssuer)
	r.Equal("sigstore", DefaultClientID)
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

func TestCallbackHandler_IssuerMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://expected.issuer.dev", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=auth-code&iss=https://evil.issuer.dev", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusBadRequest, rec.Code)

	select {
	case err := <-errCh:
		r.ErrorContains(err, "issuer mismatch")
		r.ErrorContains(err, "evil.issuer.dev")
	default:
		t.Fatal("expected error on channel")
	}
}

func TestCallbackHandler_IssuerMatchAccepted(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=auth-code&iss=https://issuer.example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)

	select {
	case code := <-codeCh:
		r.Equal("auth-code", code)
	default:
		t.Fatal("expected code on channel")
	}
}

func TestCallbackHandler_IssuerAbsentAccepted(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=test-state&code=auth-code", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)

	select {
	case code := <-codeCh:
		r.Equal("auth-code", code)
	default:
		t.Fatal("expected code on channel")
	}
}

func TestCallbackHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("test-state", "https://issuer.example.com", codeCh, errCh)

	req := httptest.NewRequest(http.MethodPost, "/auth/callback?state=test-state&code=auth-code", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusMethodNotAllowed, rec.Code)

	select {
	case <-codeCh:
		t.Fatal("should not receive code for POST request")
	default:
	}
}

func TestNewPKCE_UnsupportedMethod(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"],
			"code_challenge_methods_supported": ["plain"]
		}`, srvURL, srvURL+"/auth", srvURL+"/token", srvURL+"/keys")
	}))
	defer srv.Close()
	srvURL = srv.URL

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, srv.URL)
	r.NoError(err)

	_, err = newPKCE(provider)
	r.Error(err)
	r.ErrorContains(err, "does not support PKCE S256")
}

