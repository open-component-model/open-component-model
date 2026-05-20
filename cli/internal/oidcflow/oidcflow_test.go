package oidcflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_randomString(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	s1, err := randomString(32)
	r.NoError(err)
	r.NotEmpty(s1)

	s2, err := randomString(32)
	r.NoError(err)
	r.NotEqual(s1, s2)
}

func Test_callbackHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      string
		issuer     string
		query      string
		method     string
		wantStatus int
		wantCode   string
		wantErr    string
	}{
		{
			name:       "valid code",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state&code=auth-code-123",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantCode:   "auth-code-123",
		},
		{
			name:       "valid code with matching iss (RFC 9207)",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state&code=auth-code&iss=https://issuer.example.com",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantCode:   "auth-code",
		},
		{
			name:       "valid code without iss (RFC 9207 permissive)",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state&code=auth-code",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantCode:   "auth-code",
		},
		{
			name:       "invalid state",
			state:      "expected-state",
			issuer:     "https://issuer.example.com",
			query:      "state=wrong-state&code=auth-code",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			wantErr:    "invalid state",
		},
		{
			name:       "issuer mismatch",
			state:      "test-state",
			issuer:     "https://expected.issuer.dev",
			query:      "state=test-state&code=auth-code&iss=https://evil.issuer.dev",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			wantErr:    "issuer mismatch",
		},
		{
			name:       "missing code",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			wantErr:    "missing authorization code",
		},
		{
			name:       "IdP error",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state&error=access_denied&error_description=user+denied+consent",
			method:     http.MethodGet,
			wantStatus: http.StatusForbidden,
			wantErr:    "identity provider error",
		},
		{
			name:       "method not allowed",
			state:      "test-state",
			issuer:     "https://issuer.example.com",
			query:      "state=test-state&code=auth-code",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			codeCh := make(chan string, 1)
			errCh := make(chan error, 1)
			handler := callbackHandler(tt.state, tt.issuer, codeCh, errCh)

			req := httptest.NewRequest(tt.method, "/auth/callback?"+tt.query, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			r.Equal(tt.wantStatus, rec.Code)

			if tt.wantCode != "" {
				select {
				case code := <-codeCh:
					r.Equal(tt.wantCode, code)
				default:
					t.Fatal("expected code on channel")
				}
			}
			if tt.wantErr != "" {
				select {
				case err := <-errCh:
					r.ErrorContains(err, tt.wantErr)
				default:
					t.Fatal("expected error on channel")
				}
			}
		})
	}
}

func Test_callbackHandler_DuplicateCallback(t *testing.T) {
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
		r.Equal("first-code", code)
	default:
		t.Fatal("expected code on channel")
	}
}

func Test_waitForCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func() (context.Context, <-chan string, <-chan error)
		wantErr string
	}{
		{
			name: "success",
			setup: func() (context.Context, <-chan string, <-chan error) {
				codeCh := make(chan string, 1)
				codeCh <- "test-code"
				return context.Background(), codeCh, make(chan error)
			},
		},
		{
			name: "error from callback",
			setup: func() (context.Context, <-chan string, <-chan error) {
				errCh := make(chan error, 1)
				errCh <- errors.New("callback error")
				return context.Background(), make(chan string), errCh
			},
			wantErr: "callback error",
		},
		{
			name: "context cancelled",
			setup: func() (context.Context, <-chan string, <-chan error) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, make(chan string), make(chan error)
			},
			wantErr: "authentication cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			ctx, codeCh, errCh := tt.setup()
			code, err := waitForCode(ctx, defaultCallbackTimeout, codeCh, errCh)

			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
			} else {
				r.NoError(err)
				r.Equal("test-code", code)
			}
		})
	}
}

func Test_openBrowser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "rejects HTTP",
			url:     "http://example.com/auth",
			wantErr: "refusing to open non-HTTPS auth URL",
		},
		{
			name:    "rejects invalid URL",
			url:     "://invalid",
			wantErr: "parse auth URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			errCh := make(chan error, 1)
			err := openBrowser(t.Context(), tt.url, errCh)
			r.Error(err)
			r.ErrorContains(err, tt.wantErr)
		})
	}
}

func Test_Options_Defaults(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	r.Equal("https://oauth2.sigstore.dev/auth", DefaultIssuer)
	r.Equal("sigstore", DefaultClientID)
}
