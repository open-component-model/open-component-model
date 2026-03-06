package access_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func helmAccessResource(t *testing.T, data map[string]string) *descruntime.Resource {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)

	r := &descruntime.Resource{}
	r.Name = "mychart"
	r.Version = "0.1.0"
	r.Type = "helmChart"
	r.Access = &runtime.Raw{
		Type: runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion),
		Data: raw,
	}
	return r
}

func TestHelmAccess_GetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	h := &access.HelmAccess{}
	ctx := context.Background()

	t.Run("returns identity for HTTPS helm repository", func(t *testing.T) {
		resource := helmAccessResource(t, map[string]string{
			"helmRepository": "https://charts.example.com/stable",
		})

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "https", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
		assert.Equal(t, "stable", identity["path"])
	})

	t.Run("returns identity for HTTP helm repository", func(t *testing.T) {
		resource := helmAccessResource(t, map[string]string{
			"helmRepository": "http://charts.example.com:8080/repo",
		})

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "http", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
		assert.Equal(t, "8080", identity["port"])
		assert.Equal(t, "repo", identity["path"])
	})

	t.Run("returns identity for OCI helm repository", func(t *testing.T) {
		resource := helmAccessResource(t, map[string]string{
			"helmRepository": "oci://registry.example.com/charts/mychart:1.0.0",
		})

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "registry.example.com", identity["hostname"])
	})

	t.Run("returns nil for local helm input without repository", func(t *testing.T) {
		resource := helmAccessResource(t, map[string]string{
			"helmRepository": "",
		})

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		assert.NoError(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for invalid access type", func(t *testing.T) {
		resource := &descruntime.Resource{}
		resource.Name = "mychart"
		resource.Version = "0.1.0"
		resource.Type = "helmChart"
		resource.Access = &runtime.Raw{
			Type: runtime.NewUnversionedType("unknown"),
			Data: []byte(`{}`),
		}

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error converting resource input spec")
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		resource := helmAccessResource(t, map[string]string{
			"helmRepository": "://invalid",
		})

		identity, err := h.GetResourceCredentialConsumerIdentity(ctx, resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error parsing helm repository URL to identity")
	})
}
