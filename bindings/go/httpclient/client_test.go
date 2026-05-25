package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/httpclient"
)

func TestNew(t *testing.T) {
	t.Run("no options yields a retry client with no overall timeout", func(t *testing.T) {
		c := httpclient.New()
		require.NotNil(t, c)
		_, ok := c.Transport.(*retry.Transport)
		assert.True(t, ok, "expected *retry.Transport, got %T", c.Transport)
		assert.Zero(t, c.Timeout)
	})

	t.Run("nil config behaves like no options", func(t *testing.T) {
		c := httpclient.New(httpclient.WithConfig(nil))
		_, ok := c.Transport.(*retry.Transport)
		assert.True(t, ok)
		assert.Zero(t, c.Timeout)
	})

	t.Run("config timeouts flow into transport and overall timeout", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{
				Timeout:             httpv1alpha1.NewTimeout(90 * time.Second),
				TLSHandshakeTimeout: httpv1alpha1.NewTimeout(7 * time.Second),
				IdleConnTimeout:     httpv1alpha1.NewTimeout(45 * time.Second),
			},
		}
		c := httpclient.New(httpclient.WithConfig(cfg))

		assert.Equal(t, 90*time.Second, c.Timeout)

		rt, ok := c.Transport.(*retry.Transport)
		require.True(t, ok)
		base, ok := rt.Base.(*http.Transport)
		require.True(t, ok, "expected retry.Transport.Base to be *http.Transport, got %T", rt.Base)
		assert.Equal(t, 7*time.Second, base.TLSHandshakeTimeout)
		assert.Equal(t, 45*time.Second, base.IdleConnTimeout)
	})

	t.Run("user agent wraps the retry transport", func(t *testing.T) {
		c := httpclient.New(httpclient.WithUserAgent("test-agent/1.0"))
		// The outermost transport is the user-agent injector, not the retry transport.
		_, isRetry := c.Transport.(*retry.Transport)
		assert.False(t, isRetry, "expected user-agent transport to wrap the retry transport")
	})

	t.Run("nil Timeout leaves overall deadline unset", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{
				IdleConnTimeout: httpv1alpha1.NewTimeout(30 * time.Second),
			},
		}
		c := httpclient.New(httpclient.WithConfig(cfg))
		assert.Zero(t, c.Timeout)
	})

	t.Run("per-host config moves overall Timeout off the http.Client", func(t *testing.T) {
		// When Hosts is non-empty, the overall Timeout is applied per request
		// via a context deadline inside hostRouter (so a per-host timeout may
		// exceed the global one). http.Client.Timeout must stay zero — leaving
		// it set would cap every request at the global value before the host
		// override could take effect.
		cfg := &httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{
				Timeout: httpv1alpha1.NewTimeout(30 * time.Second),
			},
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"slow.example.com": {
					TimeoutConfig: httpv1alpha1.TimeoutConfig{
						Timeout: httpv1alpha1.NewTimeout(5 * time.Minute),
					},
				},
			},
		}
		c := httpclient.New(httpclient.WithConfig(cfg))
		assert.Zero(t, c.Timeout)
		// The transport is the host-routing transport, not the bare retry
		// transport — without that wrapping, per-host config can't be honoured.
		_, isRetry := c.Transport.(*retry.Transport)
		assert.False(t, isRetry, "expected hostRouter to wrap the retry transport when Hosts is set")
	})

}

func TestNewClient_PerHostRouting(t *testing.T) {
	// Two test servers: one whose handler stalls long enough to trip a
	// per-host responseHeaderTimeout, one with the global default that
	// answers immediately. Both URLs go through the same client; the
	// behavioural difference comes only from the per-host config.
	//
	// NewClient (no retry) is used so the failure surfaces on the first
	// attempt instead of running through retry backoff.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slow.Close)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fast.Close)

	slowHost := mustHost(t, slow.URL)

	cfg := &httpv1alpha1.Config{
		Hosts: map[string]*httpv1alpha1.HostConfig{
			slowHost: {
				TimeoutConfig: httpv1alpha1.TimeoutConfig{
					ResponseHeaderTimeout: httpv1alpha1.NewTimeout(5 * time.Millisecond),
				},
			},
		},
	}
	c := httpclient.NewClient(cfg)

	_, err := c.Get(slow.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout awaiting response headers")

	// Request to the unmatched host uses the global config (no
	// responseHeaderTimeout) and completes normally.
	resp, err := c.Get(fast.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
}

// mustHost extracts the host (host:port) from a URL or fails the test.
func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Host
}
