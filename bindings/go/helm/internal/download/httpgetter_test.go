package download

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPConfigGetter_UsesProvidedClient(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	g, err := NewHTTPConfigGetter(&http.Client{Timeout: 5 * time.Second}, HTTPConfigGetterOpts{})
	require.NoError(t, err)

	_, _ = g.Get(srv.URL)
	require.True(t, hit, "expected custom client to reach the test server")
}

func TestHTTPConfigGetter_PropagatesCredentials(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	g, err := NewHTTPConfigGetter(&http.Client{Timeout: 5 * time.Second}, HTTPConfigGetterOpts{
		username: "user",
		password: "pass",
		baseURL:  srv.URL,
	})
	require.NoError(t, err)

	_, _ = g.Get(srv.URL)
	require.NotEmpty(t, gotAuth, "expected Authorization header to be forwarded")
}

func TestHTTPConfigGetter_PropagatesUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	g, err := NewHTTPConfigGetter(&http.Client{Timeout: 5 * time.Second}, HTTPConfigGetterOpts{
		userAgent: "test-agent/1.0",
	})
	require.NoError(t, err)

	_, _ = g.Get(srv.URL)
	require.Equal(t, "test-agent/1.0", gotUA)
}

func TestHTTPConfigGetter_RespectsClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	g, err := NewHTTPConfigGetter(&http.Client{Timeout: 10 * time.Millisecond}, HTTPConfigGetterOpts{})
	require.NoError(t, err)

	_, err = g.Get(srv.URL)
	require.Error(t, err, "expected timeout error from custom client")
}
