package input_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/input"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

func wgetInputResource(t *testing.T, spec map[string]any) *constructorruntime.Resource {
	t.Helper()
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	r := &constructorruntime.Resource{}
	r.Name = "test-resource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Input = &runtime.Raw{
		Type: runtime.NewVersionedType(v1.Type, v1.Version),
		Data: raw,
	}
	return r
}

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func TestProcessResource(t *testing.T) {
	t.Parallel()

	t.Run("downloads resource as local blob", func(t *testing.T) {
		content := []byte("hello world")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(content)
		}))
		defer server.Close()

		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"url": server.URL + "/resource"})

		result, err := method.ProcessResource(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.ProcessedBlobData)
		assert.Nil(t, result.ProcessedResource)

		assert.Equal(t, content, readBlob(t, result.ProcessedBlobData))

		ma, ok := result.ProcessedBlobData.(blob.MediaTypeAware)
		require.True(t, ok)
		mt, known := ma.MediaType()
		assert.True(t, known)
		assert.Equal(t, "text/plain", mt)
	})

	t.Run("stores wget access without downloading when Reference set", func(t *testing.T) {
		var hits int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			_, _ = w.Write([]byte("should not be fetched"))
		}))
		defer server.Close()

		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{
			"url":       server.URL + "/artifact.tar.gz",
			"mediaType": "application/gzip",
			"reference": true,
		})

		result, err := method.ProcessResource(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		// Access mode returns a processed resource, not a local blob, and does not download.
		assert.Nil(t, result.ProcessedBlobData)
		require.NotNil(t, result.ProcessedResource)
		assert.Zero(t, hits, "reference mode must not download the resource")

		// The user-declared resource type is preserved.
		assert.Equal(t, "blob", result.ProcessedResource.Type)

		require.NotNil(t, result.ProcessedResource.Access, "access should be set")
		assert.Equal(t, "Wget/v1", result.ProcessedResource.Access.GetType().String(),
			"access should be a defaulted wget access spec")

		// The access is encoded as a runtime.Raw; decode it to verify the fields carried over.
		acc := &accessv1.Wget{}
		require.NoError(t, accessspec.Scheme.Convert(result.ProcessedResource.Access, acc))
		assert.Equal(t, server.URL+"/artifact.tar.gz", acc.URL)
		assert.Equal(t, "application/gzip", acc.MediaType)
	})

	t.Run("mediaType from spec overrides Content-Type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("data"))
		}))
		defer server.Close()

		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{
			"url":       server.URL + "/resource",
			"mediaType": "application/gzip",
		})

		result, err := method.ProcessResource(t.Context(), resource, nil)
		require.NoError(t, err)
		ma := result.ProcessedBlobData.(blob.MediaTypeAware)
		mt, _ := ma.MediaType()
		assert.Equal(t, "application/gzip", mt)
	})

	t.Run("applies basic auth credentials", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "user", user)
			assert.Equal(t, "pass", pass)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"url": server.URL + "/resource"})
		creds := &credv1.WgetCredentials{
			Type:     credv1.WgetCredentialsVersionedType,
			Username: "user",
			Password: "pass",
		}

		result, err := method.ProcessResource(t.Context(), resource, creds)
		require.NoError(t, err)
		assert.Equal(t, []byte("ok"), readBlob(t, result.ProcessedBlobData))
	})

	t.Run("enforces max download size", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("this body is definitely larger than the limit"))
		}))
		defer server.Close()

		method := &input.InputMethod{MaxDownloadSize: 8}
		resource := wgetInputResource(t, map[string]any{"url": server.URL + "/resource"})

		_, err := method.ProcessResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("errors on non-2xx status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"url": server.URL + "/missing"})

		_, err := method.ProcessResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("errors on missing url", func(t *testing.T) {
		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"mediaType": "text/plain"})

		_, err := method.ProcessResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("errors on unsupported scheme", func(t *testing.T) {
		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"url": "ftp://example.com/file"})

		_, err := method.ProcessResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported url scheme")
	})
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	t.Run("derives identity from url", func(t *testing.T) {
		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"url": "https://example.com/path/file.tar.gz"})

		identity, err := method.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)
		assert.Equal(t, accessspec.WgetConsumerType, identity.GetType().Name)
		assert.Equal(t, "example.com", identity[runtime.IdentityAttributeHostname])
	})

	t.Run("errors on missing url", func(t *testing.T) {
		method := &input.InputMethod{}
		resource := wgetInputResource(t, map[string]any{"mediaType": "text/plain"})

		_, err := method.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.Error(t, err)
	})
}
