package provider_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
)

func TestNewHTTPClient(t *testing.T) {
	t.Run("returns fresh client not sharing global default", func(t *testing.T) {
		client := provider.NewHTTPClient()
		assert.False(t, client == retry.DefaultClient)
	})

	t.Run("no options uses retry transport directly", func(t *testing.T) {
		client := provider.NewHTTPClient()
		_, ok := client.Transport.(*retry.Transport)
		assert.True(t, ok, "transport should be retry.Transport when no user agent set")
	})

	t.Run("disables timeout when config has zero timeout", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(0),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))
		assert.Equal(t, time.Duration(0), client.Timeout)
	})

	t.Run("applies timeout when set", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(5 * time.Minute),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))
		assert.Equal(t, 5*time.Minute, client.Timeout)
	})

	t.Run("does not wrap with user-agent when not provided", func(t *testing.T) {
		client := provider.NewHTTPClient()
		_, ok := client.Transport.(*retry.Transport)
		assert.True(t, ok, "transport should be retry.Transport without user agent wrapper")
	})

	t.Run("wraps with user-agent when provided", func(t *testing.T) {
		client := provider.NewHTTPClient(provider.WithHTTPUserAgent("test-agent/1.0"))
		// Transport should NOT be retry.Transport directly (it's wrapped).
		_, ok := client.Transport.(*retry.Transport)
		assert.False(t, ok, "transport should be wrapped with user agent transport")
	})

	t.Run("sets transport timeouts from config", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			TCPDialTimeout:        httpv1alpha1.NewTimeout(15 * time.Second),
			TCPKeepAlive:          httpv1alpha1.NewTimeout(20 * time.Second),
			TLSHandshakeTimeout:   httpv1alpha1.NewTimeout(5 * time.Second),
			ResponseHeaderTimeout: httpv1alpha1.NewTimeout(8 * time.Second),
			IdleConnTimeout:       httpv1alpha1.NewTimeout(45 * time.Second),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))

		retryTransport, ok := client.Transport.(*retry.Transport)
		require.True(t, ok)
		transport, ok := retryTransport.Base.(*http.Transport)
		require.True(t, ok)

		assert.Equal(t, 5*time.Second, transport.TLSHandshakeTimeout)
		assert.Equal(t, 8*time.Second, transport.ResponseHeaderTimeout)
		assert.Equal(t, 45*time.Second, transport.IdleConnTimeout)
	})

	t.Run("sets retry policy from config", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			RetryMinWait:  httpv1alpha1.NewTimeout(500 * time.Millisecond),
			RetryMaxWait:  httpv1alpha1.NewTimeout(10 * time.Second),
			RetryMaxRetry: intPtr(3),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))

		retryTransport, ok := client.Transport.(*retry.Transport)
		require.True(t, ok)
		require.NotNil(t, retryTransport.Policy)

		policy, ok := retryTransport.Policy().(*retry.GenericPolicy)
		require.True(t, ok)

		assert.Equal(t, 500*time.Millisecond, policy.MinWait)
		assert.Equal(t, 10*time.Second, policy.MaxWait)
		assert.Equal(t, 3, policy.MaxRetry)
	})

	t.Run("retry policy uses defaults for unset fields", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			RetryMaxRetry: intPtr(2),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))

		retryTransport, ok := client.Transport.(*retry.Transport)
		require.True(t, ok)

		policy, ok := retryTransport.Policy().(*retry.GenericPolicy)
		require.True(t, ok)

		assert.Equal(t, time.Duration(httpv1alpha1.DefaultRetryMinWait), policy.MinWait)
		assert.Equal(t, time.Duration(httpv1alpha1.DefaultRetryMaxWait), policy.MaxWait)
		assert.Equal(t, 2, policy.MaxRetry)
	})

	t.Run("applies both config and user-agent", func(t *testing.T) {
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(2 * time.Minute),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg), provider.WithHTTPUserAgent("ocm-cli/2"))
		assert.Equal(t, 2*time.Minute, client.Timeout)
		// Transport should be wrapped (not retry.Transport directly).
		_, ok := client.Transport.(*retry.Transport)
		assert.False(t, ok)
	})
}

func intPtr(v int) *int {
	return &v
}
