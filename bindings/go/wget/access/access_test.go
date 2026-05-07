package access_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/access/spec/v1"
)

func TestScheme_Convert(t *testing.T) {
	t.Parallel()

	t.Run("converts versioned type", func(t *testing.T) {
		raw := &runtime.Raw{
			Type: runtime.NewVersionedType("wget", v1.Version),
			Data: []byte(`{"url":"https://example.com/file.tar.gz"}`),
		}

		wget := v1.Wget{}
		err := access.Scheme.Convert(raw, &wget)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/file.tar.gz", wget.URL)
	})

	t.Run("converts unversioned type", func(t *testing.T) {
		raw := &runtime.Raw{
			Type: runtime.NewUnversionedType("wget"),
			Data: []byte(`{"url":"https://example.com/file.tar.gz"}`),
		}

		wget := v1.Wget{}
		err := access.Scheme.Convert(raw, &wget)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/file.tar.gz", wget.URL)
	})

	t.Run("rejects unknown type", func(t *testing.T) {
		raw := &runtime.Raw{
			Type: runtime.NewUnversionedType("unknown"),
			Data: []byte(`{}`),
		}

		wget := v1.Wget{}
		err := access.Scheme.Convert(raw, &wget)
		assert.Error(t, err)
	})
}

func wgetAccessResource(t *testing.T, data map[string]any) *descruntime.Resource {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)

	r := &descruntime.Resource{}
	r.Name = "myresource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Access = &runtime.Raw{
		Type: runtime.NewVersionedType("wget", v1.Version),
		Data: raw,
	}
	return r
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	repo := access.NewWgetAccess()

	t.Run("returns identity for HTTPS URL", func(t *testing.T) {
		resource := wgetAccessResource(t, map[string]any{
			"url": "https://example.com/path/to/resource",
		})

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "wget", identity["type"])
		assert.Equal(t, "https", identity["scheme"])
		assert.Equal(t, "example.com", identity["hostname"])
		assert.Equal(t, "path/to/resource", identity["path"])
	})

	t.Run("returns identity for HTTP URL with port", func(t *testing.T) {
		resource := wgetAccessResource(t, map[string]any{
			"url": "http://example.com:8080/file.tar.gz",
		})

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "wget", identity["type"])
		assert.Equal(t, "http", identity["scheme"])
		assert.Equal(t, "example.com", identity["hostname"])
		assert.Equal(t, "8080", identity["port"])
		assert.Equal(t, "file.tar.gz", identity["path"])
	})

	t.Run("returns error for nil resource", func(t *testing.T) {
		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), nil)
		assert.Error(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		resource := &descruntime.Resource{}
		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for invalid access type", func(t *testing.T) {
		resource := &descruntime.Resource{}
		resource.Name = "myresource"
		resource.Access = &runtime.Raw{
			Type: runtime.NewUnversionedType("unknown"),
			Data: []byte(`{}`),
		}

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error converting resource access spec")
	})

	t.Run("returns error for empty URL", func(t *testing.T) {
		resource := wgetAccessResource(t, map[string]any{
			"url": "",
		})

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		resource := wgetAccessResource(t, map[string]any{
			"url": "://invalid",
		})

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error parsing wget URL to identity")
	})
}
