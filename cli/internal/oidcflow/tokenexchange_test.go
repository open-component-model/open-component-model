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

func TestExchangeToken_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       ExchangeOptions
		handler    http.HandlerFunc
		errContains string
	}{
		{
			name:        "empty TokenURL",
			opts:        ExchangeOptions{SubjectToken: "tok"},
			errContains: "TokenURL is required",
		},
		{
			name:        "empty SubjectToken",
			opts:        ExchangeOptions{TokenURL: "https://sts.example.com/token"},
			errContains: "SubjectToken is required",
		},
		{
			name: "HTTP error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_grant"}`))
			},
			errContains: "HTTP 400",
		},
		{
			name: "missing access_token",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"token_type": "N_A"})
			},
			errContains: "missing access_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			opts := tt.opts
			if tt.handler != nil {
				srv := httptest.NewServer(tt.handler)
				defer srv.Close()
				opts.TokenURL = srv.URL
				opts.SubjectToken = "tok"
				opts.HTTPClient = srv.Client()
			}

			_, err := ExchangeToken(t.Context(), opts)
			r.Error(err)
			r.Contains(err.Error(), tt.errContains)
		})
	}
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
