package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	t.Run("returns HelmChartRepository identity for HTTPS repository", func(t *testing.T) {
		identity, err := internal.CredentialConsumerIdentity("https://charts.example.com/stable")
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType), identity.GetType())
		assert.Equal(t, "https", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
	})

	t.Run("returns HelmChartRepository identity for HTTP repository", func(t *testing.T) {
		identity, err := internal.CredentialConsumerIdentity("http://charts.example.com:8080/repo")
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType), identity.GetType())
		assert.Equal(t, "http", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
		assert.Equal(t, "8080", identity["port"])
	})

	t.Run("returns OCIRegistry identity for OCI repository", func(t *testing.T) {
		identity, err := internal.CredentialConsumerIdentity("oci://registry.example.com/charts/mychart:1.0.0")
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, ocicredentialsspecv1.Type, identity.GetType())
		assert.Equal(t, "oci", identity["scheme"])
		assert.Equal(t, "registry.example.com", identity["hostname"])
	})

	t.Run("returns error for empty repository", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helm repository URL is required")
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("://invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error parsing helm repository URL to identity")
	})
}
