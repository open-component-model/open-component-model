package httpclient_test

import (
	"net/http"
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
}
