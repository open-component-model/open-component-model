package oidcflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExchangeToken_Success(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Equal(http.MethodPost, req.Method)
		r.Equal("application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
		r.NoError(req.ParseForm())
		r.Equal(grantTypeTokenExchange, req.PostForm.Get("grant_type"))
		r.Equal("my-machine-token", req.PostForm.Get("subject_token"))
		r.Equal(tokenTypeJWT, req.PostForm.Get("subject_token_type"))
		r.Equal("sigstore", req.PostForm.Get("audience"))
		r.Equal(tokenTypeIDToken, req.PostForm.Get("requested_token_type"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"access_token":      "oidc-id-token-value",
			"token_type":        "N_A",
			"issued_token_type": tokenTypeIDToken,
		})
	}))
	defer srv.Close()

	token, err := ExchangeToken(t.Context(), ExchangeOptions{
		TokenURL:     srv.URL,
		SubjectToken: "my-machine-token",
		HTTPClient:   srv.Client(),
	})
	r.NoError(err)
	r.Equal("oidc-id-token-value", token.RawToken)
}

func TestExchangeToken_CustomOptions(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.NoError(req.ParseForm())
		r.Equal("custom-audience", req.PostForm.Get("audience"))
		r.Equal("urn:ietf:params:oauth:token-type:access_token", req.PostForm.Get("subject_token_type"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "custom-token"})
	}))
	defer srv.Close()

	token, err := ExchangeToken(t.Context(), ExchangeOptions{
		TokenURL:         srv.URL,
		SubjectToken:     "tok",
		SubjectTokenType: "urn:ietf:params:oauth:token-type:access_token",
		Audience:         "custom-audience",
		HTTPClient:       srv.Client(),
	})
	r.NoError(err)
	r.Equal("custom-token", token.RawToken)
}

func TestExchangeToken_HTTPError(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"token expired"}`))
	}))
	defer srv.Close()

	_, err := ExchangeToken(t.Context(), ExchangeOptions{
		TokenURL:     srv.URL,
		SubjectToken: "expired-token",
		HTTPClient:   srv.Client(),
	})
	r.Error(err)
	r.Contains(err.Error(), "HTTP 400")
	r.Contains(err.Error(), "invalid_grant")
}

func TestExchangeToken_MissingAccessToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token_type": "N_A"})
	}))
	defer srv.Close()

	_, err := ExchangeToken(t.Context(), ExchangeOptions{
		TokenURL:     srv.URL,
		SubjectToken: "tok",
		HTTPClient:   srv.Client(),
	})
	r.Error(err)
	r.Contains(err.Error(), "missing access_token")
}

func TestExchangeToken_ContextCancellation(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ExchangeToken(ctx, ExchangeOptions{
		TokenURL:     srv.URL,
		SubjectToken: "tok",
		HTTPClient:   srv.Client(),
	})
	r.Error(err)
}

func TestExchangeToken_EmptyTokenURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ExchangeToken(t.Context(), ExchangeOptions{
		SubjectToken: "tok",
	})
	r.Error(err)
	r.Contains(err.Error(), "TokenURL is required")
}

func TestExchangeToken_EmptySubjectToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ExchangeToken(t.Context(), ExchangeOptions{
		TokenURL: "https://sts.example.com/token",
	})
	r.Error(err)
	r.Contains(err.Error(), "SubjectToken is required")
}
