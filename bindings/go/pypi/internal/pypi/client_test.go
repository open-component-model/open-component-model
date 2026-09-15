package pypi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/pypi/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func typedCreds(username, password, token string) *credsv1.PyPICredentials {
	return &credsv1.PyPICredentials{
		Type:          runtime.NewVersionedType(credsv1.PyPICredentialsType, credsv1.Version),
		Username:      username,
		Password:      password,
		IdentityToken: token,
	}
}

func TestClientGet_Auth(t *testing.T) {
	t.Run("identityToken yields Bearer auth and sends accept header", func(t *testing.T) {
		var auth, accept string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth, accept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		}))
		t.Cleanup(srv.Close)
		resp, err := NewClient(srv.Client()).Get(context.Background(), srv.URL, AcceptHeader, typedCreds("", "", "tok"))
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, "Bearer tok", auth)
		assert.Equal(t, AcceptHeader, accept)
	})
	t.Run("username+password yields Basic auth", func(t *testing.T) {
		var user, pass string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, _ = r.BasicAuth()
		}))
		t.Cleanup(srv.Close)
		resp, err := NewClient(srv.Client()).Get(context.Background(), srv.URL, "", typedCreds("__token__", "test-value", ""))
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, "__token__", user)
		assert.Equal(t, "test-value", pass)
	})
	t.Run("nil credentials are anonymous", func(t *testing.T) {
		var auth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
		}))
		t.Cleanup(srv.Close)
		resp, err := NewClient(srv.Client()).Get(context.Background(), srv.URL, "", nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Empty(t, auth)
	})
	t.Run("password-only credentials are rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		_, err := NewClient(srv.Client()).Get(context.Background(), srv.URL, "", typedCreds("", "test-value", ""))
		require.ErrorIs(t, err, ErrUnusableCredentials)
	})
}
