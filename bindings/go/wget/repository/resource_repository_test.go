package repository_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

func wgetResource(t *testing.T, serverURL string, spec map[string]any) *descruntime.Resource {
	t.Helper()
	if spec["url"] == nil {
		spec["url"] = serverURL + "/resource"
	}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	r := &descruntime.Resource{}
	r.Name = "test-resource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Access = &runtime.Raw{
		Type: runtime.NewVersionedType("wget", v1.Version),
		Data: raw,
	}
	return r
}

func TestDownloadResource(t *testing.T) {
	t.Parallel()

	t.Run("downloads resource with GET", func(t *testing.T) {
		content := []byte("hello world")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "text/plain")
			w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, b)

		data := readBlob(t, b)
		assert.Equal(t, content, data)

		if ma, ok := b.(blob.MediaTypeAware); ok {
			mt, known := ma.MediaType()
			assert.True(t, known)
			assert.Equal(t, "text/plain", mt)
		}
	})

	t.Run("forwards credentials to the downloader", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "myuser", user)
			assert.Equal(t, "mypass", pass)
			w.Write([]byte("authenticated"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		creds := &credv1.WgetCredentials{
			Type:     runtime.NewVersionedType(credv1.WgetCredentialsType, credv1.Version),
			Username: "myuser",
			Password: "mypass",
		}
		b, err := repo.DownloadResource(t.Context(), resource, creds)
		require.NoError(t, err)
		assert.Equal(t, []byte("authenticated"), readBlob(t, b))
	})

	t.Run("passes the max download size to the downloader", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello world")) // 11 bytes, limit is 10
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(
			repository.WithHTTPClient(server.Client()),
			repository.WithMaxDownloadSize(10),
		)
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("returns error for nil resource", func(t *testing.T) {
		repo := repository.NewResourceRepository()
		_, err := repo.DownloadResource(t.Context(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource is required")
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		repo := repository.NewResourceRepository()
		resource := &descruntime.Resource{}
		resource.Name = "test"
		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource access is required")
	})
}

func TestUploadResource(t *testing.T) {
	t.Parallel()

	repo := repository.NewResourceRepository()
	_, err := repo.UploadResource(t.Context(), nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	repo := repository.NewResourceRepository()

	resource := &descruntime.Resource{}
	resource.Name = "test"
	resource.Version = "1.0.0"
	resource.Type = "blob"
	raw, _ := json.Marshal(map[string]any{"url": "https://example.com:443/path/file.tar.gz"})
	resource.Access = &runtime.Raw{
		Type: runtime.NewVersionedType("wget", v1.Version),
		Data: raw,
	}

	identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
	require.NoError(t, err)
	assert.Equal(t, "Wget", identity["type"])
	assert.Equal(t, "https", identity["scheme"])
	assert.Equal(t, "example.com", identity["hostname"])
	assert.Equal(t, "443", identity["port"])
	assert.Equal(t, "path/file.tar.gz", identity["path"])
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	t.Parallel()

	repo := repository.NewResourceRepository()
	scheme := repo.GetResourceRepositoryScheme()
	require.NotNil(t, scheme)
	assert.True(t, scheme.IsRegistered(runtime.NewVersionedType("wget", v1.Version)))
	assert.True(t, scheme.IsRegistered(runtime.NewUnversionedType("wget")))
}

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func(rc io.ReadCloser) {
		_ = rc.Close()
	}(rc)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func TestProcessResourceDigest(t *testing.T) {
	t.Parallel()

	t.Run("computes digest by downloading the content once", func(t *testing.T) {
		content := []byte("hello digest world")
		var hits int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			_, _ = w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})

		processed, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, processed.Digest)
		assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
		assert.Equal(t, godigest.FromBytes(content).Encoded(), processed.Digest.Value)
		assert.Equal(t, 1, hits, "digest processing should download the content exactly once")
		assert.Nil(t, resource.Digest, "the input resource must not be mutated")
	})

	t.Run("verifies a matching pre-existing digest", func(t *testing.T) {
		content := []byte("verify me")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  godigest.FromBytes(content).Encoded(),
		}

		_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
	})

	t.Run("fails on digest mismatch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("actual content"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  godigest.FromBytes([]byte("different content")).Encoded(),
		}

		_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "digest mismatch")
	})
}
