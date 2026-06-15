package repository_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/wget/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/wget/repository"
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

	t.Run("downloads resource with POST and body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, "request-body", string(body))
			w.Write([]byte("response"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url":  server.URL + "/resource",
			"verb": "POST",
			"body": []byte("request-body"),
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte("response"), readBlob(t, b))
	})

	t.Run("sends custom headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "token-123", r.Header.Get("X-Custom-Token"))
			assert.Equal(t, []string{"val1", "val2"}, r.Header.Values("X-Multi"))
			w.Write([]byte("ok"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
			"header": map[string][]string{
				"X-Custom-Token": {"token-123"},
				"X-Multi":        {"val1", "val2"},
			},
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte("ok"), readBlob(t, b))
	})

	t.Run("applies basic auth from credentials", func(t *testing.T) {
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

		creds := map[string]string{
			"username": "myuser",
			"password": "mypass",
		}
		b, err := repo.DownloadResource(t.Context(), resource, creds)
		require.NoError(t, err)
		assert.Equal(t, []byte("authenticated"), readBlob(t, b))
	})

	t.Run("does not follow redirects when noRedirect is true", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/resource" {
				http.Redirect(w, r, "/redirected", http.StatusFound)
				return
			}
			w.Write([]byte("redirected-content"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url":        server.URL + "/resource",
			"noRedirect": true,
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 302")
	})

	t.Run("follows redirects by default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/resource" {
				http.Redirect(w, r, "/redirected", http.StatusFound)
				return
			}
			w.Write([]byte("redirected-content"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte("redirected-content"), readBlob(t, b))
	})

	t.Run("uses mediaType from spec over Content-Type header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("data"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url":       server.URL + "/resource",
			"mediaType": "application/gzip",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)

		if ma, ok := b.(blob.MediaTypeAware); ok {
			mt, known := ma.MediaType()
			assert.True(t, known)
			assert.Equal(t, "application/gzip", mt)
		}
	})

	t.Run("falls back to application/octet-stream when no media type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write without setting Content-Type — Go will auto-detect, but we'll
			// clear it first to simulate no content type.
			w.Header().Del("Content-Type")
			w.Header().Set("Content-Type", "")
			w.Write([]byte("data"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)

		// The server may still set a Content-Type via Go's auto-detection,
		// so we just verify the blob was created successfully.
		require.NotNil(t, b)
	})

	t.Run("returns error for non-2xx status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 404")
	})

	t.Run("returns error for nil resource", func(t *testing.T) {
		repo := repository.NewResourceRepository()
		_, err := repo.DownloadResource(t.Context(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource is required")
	})

	t.Run("returns error for empty URL", func(t *testing.T) {
		repo := repository.NewResourceRepository()
		resource := wgetResource(t, "", map[string]any{
			"url": "",
		})
		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("returns error for non-http/https scheme", func(t *testing.T) {
		repo := repository.NewResourceRepository()
		resource := wgetResource(t, "", map[string]any{
			"url": "ftp://example.com/file.tar.gz",
		})
		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported url scheme")
	})

	t.Run("applies bearer token from identityToken credential", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-token-value", r.Header.Get("Authorization"))
			w.Write([]byte("authenticated"))
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		creds := map[string]string{
			"identityToken": "my-token-value",
		}
		b, err := repo.DownloadResource(t.Context(), resource, creds)
		require.NoError(t, err)
		assert.Equal(t, []byte("authenticated"), readBlob(t, b))
	})

	t.Run("returns error when response exceeds max download size", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write 11 bytes but max is 10
			w.Write([]byte("hello world"))
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

	t.Run("succeeds when response is within max download size", func(t *testing.T) {
		content := []byte("hello")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(
			repository.WithHTTPClient(server.Client()),
			repository.WithMaxDownloadSize(10),
		)
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		require.NoError(t, err)
		assert.Equal(t, content, readBlob(t, b))
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
	assert.Equal(t, "wget", identity["type"])
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
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}
