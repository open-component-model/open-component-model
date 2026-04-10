package integration_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/wget/repository"
)

// newFileServer starts an httptest server that serves files from the given directory,
// mirroring the pattern used by helm transformation tests.
func newFileServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv
}

// newAuthServer starts an httptest server that requires Basic Auth and serves files from dir.
func newAuthServer(t *testing.T, dir string, username, password string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wgetResource(t *testing.T, spec map[string]any) *descruntime.Resource {
	t.Helper()
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

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// setupTestData creates a temporary directory with test files for serving.
func setupTestData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Plain text file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("Hello, World!\n"), 0o644))

	// Binary file
	binaryData := make([]byte, 1024)
	for i := range binaryData {
		binaryData[i] = byte(i % 256)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.bin"), binaryData, 0o644))

	// Gzipped file
	gzPath := filepath.Join(dir, "archive.tar.gz")
	f, err := os.Create(gzPath)
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	_, err = gz.Write([]byte("compressed content for testing"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())

	// Nested path
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub", "dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "dir", "nested.json"), []byte(`{"key":"value"}`), 0o644))

	return dir
}

func Test_Integration_WgetResourceRepository(t *testing.T) {
	t.Parallel()

	testDataDir := setupTestData(t)
	srv := newFileServer(t, testDataDir)
	t.Logf("File server at %s", srv.URL)

	repo := repository.NewResourceRepository(repository.WithHTTPClient(srv.Client()))

	t.Run("download plain text file and verify content", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)
		r.NotNil(b)

		data := readBlob(t, b)
		assert.Equal(t, "Hello, World!\n", string(data))

		// Verify media type was detected from Content-Type
		if ma, ok := b.(blob.MediaTypeAware); ok {
			mt, known := ma.MediaType()
			assert.True(t, known)
			assert.Contains(t, mt, "text/plain")
		}
	})

	t.Run("download binary file and verify integrity via digest", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/data.bin",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		data := readBlob(t, b)
		assert.Len(t, data, 1024)

		// Verify content integrity
		originalData, err := os.ReadFile(filepath.Join(testDataDir, "data.bin"))
		r.NoError(err)
		assert.Equal(t, sha256sum(originalData), sha256sum(data))

		// Verify blob reports correct size
		if sa, ok := b.(blob.SizeAware); ok {
			assert.Equal(t, int64(1024), sa.Size())
		}

		// Verify blob digest is computed
		if da, ok := b.(blob.DigestAware); ok {
			digest, known := da.Digest()
			assert.True(t, known)
			assert.NotEmpty(t, digest)
		}
	})

	t.Run("download gzipped file with explicit media type", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url":       srv.URL + "/archive.tar.gz",
			"mediaType": "application/x-tar+gzip",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		data := readBlob(t, b)
		r.NotEmpty(data)

		// Verify the explicit media type takes precedence
		if ma, ok := b.(blob.MediaTypeAware); ok {
			mt, known := ma.MediaType()
			assert.True(t, known)
			assert.Equal(t, "application/x-tar+gzip", mt)
		}

		// Verify it's actually gzipped by decompressing
		rc, err := b.ReadCloser()
		r.NoError(err)
		defer rc.Close()
		gz, err := gzip.NewReader(rc)
		r.NoError(err)
		decompressed, err := io.ReadAll(gz)
		r.NoError(err)
		assert.Equal(t, "compressed content for testing", string(decompressed))
	})

	t.Run("download file from nested path", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url":       srv.URL + "/sub/dir/nested.json",
			"mediaType": "application/json",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		data := readBlob(t, b)
		assert.JSONEq(t, `{"key":"value"}`, string(data))
	})

	t.Run("download with custom headers", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		// Server that echoes back the custom header in the response body
		echoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "X-Request-ID: %s", req.Header.Get("X-Request-ID"))
		}))
		t.Cleanup(echoSrv.Close)

		echoRepo := repository.NewResourceRepository(repository.WithHTTPClient(echoSrv.Client()))
		resource := wgetResource(t, map[string]any{
			"url": echoSrv.URL + "/echo",
			"header": map[string][]string{
				"X-Request-ID": {"test-12345"},
			},
		})

		b, err := echoRepo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		data := readBlob(t, b)
		assert.Equal(t, "X-Request-ID: test-12345", string(data))
	})

	t.Run("download with POST verb and body", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		// Server that echoes the request method and body
		echoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			body, _ := io.ReadAll(req.Body)
			w.Header().Set("Content-Type", "application/json")
			resp, _ := json.Marshal(map[string]string{
				"method": req.Method,
				"body":   string(body),
			})
			w.Write(resp)
		}))
		t.Cleanup(echoSrv.Close)

		echoRepo := repository.NewResourceRepository(repository.WithHTTPClient(echoSrv.Client()))
		resource := wgetResource(t, map[string]any{
			"url":  echoSrv.URL + "/api",
			"verb": "POST",
			"body": []byte(`{"query":"test"}`),
		})

		b, err := echoRepo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		var resp map[string]string
		r.NoError(json.Unmarshal(readBlob(t, b), &resp))
		assert.Equal(t, "POST", resp["method"])
		assert.Equal(t, `{"query":"test"}`, resp["body"])
	})

	t.Run("credential consumer identity resolution", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		r.NoError(err)
		r.NotNil(identity)

		assert.Equal(t, "wget", identity["type"])
		assert.Equal(t, "http", identity["scheme"])
		assert.NotEmpty(t, identity["hostname"])
		assert.NotEmpty(t, identity["port"])
	})

	t.Run("blob is re-readable", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)

		// Read the blob twice to verify it can be re-read
		data1 := readBlob(t, b)
		data2 := readBlob(t, b)
		assert.Equal(t, data1, data2)
		assert.Equal(t, "Hello, World!\n", string(data1))
	})

	t.Run("returns error for non-existent path", func(t *testing.T) {
		t.Parallel()

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/does-not-exist",
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 404")
	})

	t.Run("upload is not supported", func(t *testing.T) {
		t.Parallel()

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		_, err := repo.UploadResource(t.Context(), resource, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported")
	})
}

func Test_Integration_WgetWithAuthentication(t *testing.T) {
	t.Parallel()

	testDataDir := setupTestData(t)
	const testUser = "ocm-user"
	const testPass = "s3cret-p@ssw0rd!"

	srv := newAuthServer(t, testDataDir, testUser, testPass)
	t.Logf("Authenticated file server at %s", srv.URL)

	repo := repository.NewResourceRepository(repository.WithHTTPClient(srv.Client()))

	t.Run("download succeeds with valid credentials", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		creds := map[string]string{
			"username": testUser,
			"password": testPass,
		}

		b, err := repo.DownloadResource(t.Context(), resource, creds)
		r.NoError(err)
		assert.Equal(t, "Hello, World!\n", string(readBlob(t, b)))
	})

	t.Run("download fails without credentials", func(t *testing.T) {
		t.Parallel()

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 401")
	})

	t.Run("download fails with wrong credentials", func(t *testing.T) {
		t.Parallel()

		resource := wgetResource(t, map[string]any{
			"url": srv.URL + "/hello.txt",
		})

		creds := map[string]string{
			"username": "wrong",
			"password": "wrong",
		}

		_, err := repo.DownloadResource(t.Context(), resource, creds)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 401")
	})
}

func Test_Integration_WgetWithRedirects(t *testing.T) {
	t.Parallel()

	testDataDir := setupTestData(t)
	fileSrv := newFileServer(t, testDataDir)

	// Redirect server that redirects to the file server
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := fileSrv.URL + r.URL.Path
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirectSrv.Close)

	// Use a shared client that trusts both servers
	repo := repository.NewResourceRepository()

	t.Run("follows redirects by default", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		resource := wgetResource(t, map[string]any{
			"url": redirectSrv.URL + "/hello.txt",
		})

		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)
		assert.Equal(t, "Hello, World!\n", string(readBlob(t, b)))
	})

	t.Run("stops at redirect when noRedirect is set", func(t *testing.T) {
		t.Parallel()

		resource := wgetResource(t, map[string]any{
			"url":        redirectSrv.URL + "/hello.txt",
			"noRedirect": true,
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 307")
	})
}

func Test_Integration_WgetSchemeRegistration(t *testing.T) {
	t.Parallel()

	t.Run("scheme resolves versioned type", func(t *testing.T) {
		r := require.New(t)

		typ := runtime.NewVersionedType("wget", v1.Version)
		r.True(access.Scheme.IsRegistered(typ))

		obj, err := access.Scheme.NewObject(typ)
		r.NoError(err)

		wget, ok := obj.(*v1.Wget)
		r.True(ok)
		r.NotNil(wget)
	})

	t.Run("scheme resolves unversioned type", func(t *testing.T) {
		r := require.New(t)

		typ := runtime.NewUnversionedType("wget")
		r.True(access.Scheme.IsRegistered(typ))
	})

	t.Run("round-trip through scheme convert", func(t *testing.T) {
		r := require.New(t)

		original := &v1.Wget{
			Type:       runtime.NewVersionedType("wget", v1.Version),
			URL:        "https://example.com/resource.tar.gz",
			MediaType:  "application/x-tar+gzip",
			Header:     map[string][]string{"Authorization": {"Bearer token"}},
			Verb:       "POST",
			Body:       []byte("request body"),
			NoRedirect: true,
		}

		data, err := json.Marshal(original)
		r.NoError(err)

		raw := &runtime.Raw{
			Type: original.GetType(),
			Data: data,
		}

		converted := &v1.Wget{}
		r.NoError(access.Scheme.Convert(raw, converted))

		assert.Equal(t, original.URL, converted.URL)
		assert.Equal(t, original.MediaType, converted.MediaType)
		assert.Equal(t, original.Header, converted.Header)
		assert.Equal(t, original.Verb, converted.Verb)
		assert.Equal(t, original.Body, converted.Body)
		assert.Equal(t, original.NoRedirect, converted.NoRedirect)
	})

	t.Run("JSON schema is available", func(t *testing.T) {
		r := require.New(t)

		schema := v1.Wget{}.JSONSchema()
		r.NotEmpty(schema)

		var parsed map[string]any
		r.NoError(json.Unmarshal(schema, &parsed))
		assert.Equal(t, "Wget", parsed["title"])

		props, ok := parsed["properties"].(map[string]any)
		r.True(ok)
		assert.Contains(t, props, "url")
		assert.Contains(t, props, "mediaType")
		assert.Contains(t, props, "header")
		assert.Contains(t, props, "verb")
		assert.Contains(t, props, "body")
		assert.Contains(t, props, "noRedirect")

		required, ok := parsed["required"].([]any)
		r.True(ok)
		assert.Contains(t, required, "type")
		assert.Contains(t, required, "url")
	})
}

func Test_Integration_WgetPluginRegistration(t *testing.T) {
	t.Parallel()

	// Verify the ResourceRepository satisfies BuiltinResourceRepository
	// by checking scheme is returned and types are registered.
	repo := repository.NewResourceRepository()
	scheme := repo.GetResourceRepositoryScheme()

	require.NotNil(t, scheme)

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType("wget", v1.Version),
		runtime.NewUnversionedType("wget"),
	} {
		t.Run(fmt.Sprintf("scheme has type %s", typ), func(t *testing.T) {
			assert.True(t, scheme.IsRegistered(typ))

			obj, err := scheme.NewObject(typ)
			require.NoError(t, err)

			_, ok := obj.(*v1.Wget)
			assert.True(t, ok)
		})
	}

	// Verify GetTypes returns the expected types for plugin registry enumeration
	types := scheme.GetTypes()
	found := false
	for typ := range types {
		if strings.Contains(typ.Name, "wget") {
			found = true
			break
		}
	}
	assert.True(t, found, "scheme should contain wget type")
}
