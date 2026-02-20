package provider_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
)

func TestCreateHTTPClient(t *testing.T) {
	t.Run("disables timeout when config has zero timeout", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(0),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))
		r.False(client == retry.DefaultClient, "should return a new client, not the default singleton")
		r.Equal(time.Duration(0), client.Timeout)
	})

	t.Run("applies timeout when set", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(5 * time.Minute),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg))
		r.NotEqual(retry.DefaultClient, client)
		r.Equal(5*time.Minute, client.Timeout)
	})

	t.Run("applies user-agent when set", func(t *testing.T) {
		r := require.New(t)
		client := provider.NewHTTPClient(provider.WithHTTPUserAgent("test-agent/1.0"))
		r.NotEqual(retry.DefaultClient, client)
		r.NotNil(client.Transport)
	})

	t.Run("applies both timeout and user-agent", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			Timeout: httpv1alpha1.NewTimeout(2 * time.Minute),
		}
		client := provider.NewHTTPClient(provider.WithHttpConfig(cfg), provider.WithHTTPUserAgent("ocm-cli/2"))
		r.NotEqual(retry.DefaultClient, client)
		r.Equal(2*time.Minute, client.Timeout)
		r.NotNil(client.Transport)
	})
}
