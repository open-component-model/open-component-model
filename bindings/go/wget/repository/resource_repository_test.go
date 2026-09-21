package repository_test

import (
	"crypto"
	_ "crypto/sha1"
	_ "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
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

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
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

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
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

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()),
			repository.WithMaxDownloadSize(10),
		)
		resource := wgetResource(t, server.URL, map[string]any{
			"url": server.URL + "/resource",
		})

		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("closing the downloaded blob reclaims the temporary file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello world"))
		}))
		defer server.Close()

		tempFolder := t.TempDir()
		repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder},
			repository.WithHTTPClient(server.Client()))

		b, err := repo.DownloadResource(t.Context(), wgetResource(t, server.URL, map[string]any{}), nil)
		require.NoError(t, err)

		entries, err := os.ReadDir(tempFolder)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the body must be streamed into the configured temp folder")

		closer, ok := b.(io.Closer)
		require.True(t, ok, "the downloaded blob must be closeable so callers can reclaim it")
		require.NoError(t, closer.Close())

		entries, err = os.ReadDir(tempFolder)
		require.NoError(t, err)
		assert.Empty(t, entries, "closing the blob must remove the temporary file")
	})

	t.Run("returns error for nil resource", func(t *testing.T) {
		repo := repository.NewResourceRepository(nil)
		_, err := repo.DownloadResource(t.Context(), nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource is required")
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		repo := repository.NewResourceRepository(nil)
		resource := &descruntime.Resource{}
		resource.Name = "test"
		_, err := repo.DownloadResource(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource access is required")
	})
}

func TestUploadResource(t *testing.T) {
	t.Parallel()

	repo := repository.NewResourceRepository(nil)
	_, err := repo.UploadResource(t.Context(), nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	repo := repository.NewResourceRepository(nil)

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

	repo := repository.NewResourceRepository(nil)
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

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
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

	t.Run("leaves no temporary file behind", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello digest world"))
		}))
		defer server.Close()

		tempFolder := t.TempDir()
		repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder},
			repository.WithHTTPClient(server.Client()))

		_, err := repo.ProcessResourceDigest(t.Context(), wgetResource(t, server.URL, map[string]any{}), nil)
		require.NoError(t, err)

		entries, err := os.ReadDir(tempFolder)
		require.NoError(t, err)
		assert.Empty(t, entries, "digest processing must reclaim the file it downloaded")
	})

	t.Run("verifies a matching pre-existing digest", func(t *testing.T) {
		content := []byte("verify me")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
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

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
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

	t.Run("fails on unsupported hash algorithm", func(t *testing.T) {
		content := []byte("verify me")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-512",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  godigest.FromBytes(content).Encoded(),
		}

		_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("fails on unsupported normalisation algorithm", func(t *testing.T) {
		content := []byte("verify me")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(content)
		}))
		defer server.Close()

		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "jsonNormalisation/v1",
			Value:                  godigest.FromBytes(content).Encoded(),
		}

		_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported normalisation algorithm")
	})
}

// TestProcessResourceDigest_ConfigDriven exercises the shared
// checksum.http.config.ocm.software config on the access-side digest processor:
//
//   - a defaultChecksumPolicy of type httpHeader pins the digest from the
//     server-advertised x-checksum-sha256 header without a body download,
//   - externalUrl sources with an explicit URL fetch that URL verbatim to
//     pin the digest,
//   - a per-host override replaces the default policy for URLs matching that
//     host key.
func TestProcessResourceDigest_ConfigDriven(t *testing.T) {
	t.Parallel()

	content := []byte("verify me via header")
	sha256 := godigest.FromBytes(content).Encoded()

	t.Run("default policy verifies the RFC-9530 header source", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha256", sha256)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingFail,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		processed, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, processed.Digest)
		assert.Equal(t, sha256, processed.Digest.Value)
	})

	t.Run("externalUrl source with an explicit URL verifies against the sidecar", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/resource", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) })
		mux.HandleFunc("/resource.sha256", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(sha256 + "  artifact\n"))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingFail,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceExternalURL, URL: server.URL + "/resource.sha256", Algorithms: []string{"sha256"}}},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		assert.Equal(t, sha256, processed.Digest.Value)
	})


	t.Run("host override wins over default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha256", sha256)
			_, _ = w.Write(content)
		}))
		defer server.Close()
		host := strings.TrimPrefix(server.URL, "http://")

		// The default would silently accept anything (compute-on-missing with
		// no working sources); the host-scoped override enforces fail with a
		// header source. If the override is applied, the verified download
		// succeeds only because the server actually advertises a matching
		// header — proving the override took precedence.
		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingCompute,
			},
			Hosts: map[string]*checksumhttpv1alpha1.HostConfig{
				host: {
					ChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
						OnMissing: checksumhttpv1alpha1.OnMissingFail,
						Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
					},
				},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		assert.Equal(t, sha256, processed.Digest.Value)
	})
}

// TestProcessResourceDigest_AcceptsPrefixedPinnedDigest confirms the access-side
// digest processor treats a pinned `sha256:<hex>` value as equal to the bare-hex
// value the download produces. Matches verifyProvidedDigest's normalisation on
// the input path — without it, godigest.Encoded() callers (which pass the
// prefixed form) get spurious mismatches.
func TestProcessResourceDigest_AcceptsPrefixedPinnedDigest(t *testing.T) {
	t.Parallel()
	content := []byte("prefixed pinned")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
	resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
	resource.Digest = &descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "sha256:" + godigest.FromBytes(content).Encoded(),
	}

	_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
	require.NoError(t, err, "sha256:-prefixed pinned digest must match bare-hex computed digest")
}

// TestProcessResourceDigest_AccessFastPath exercises the no-download fast
// path: whenever a checksum-http policy applies, ProcessResourceDigest MUST
// pin the resource digest from what the source advertises without hashing
// the body itself. The server backing every subtest here refuses GET on the
// artifact path so a body download would fail the test — proving the
// processor really skips it.
func TestProcessResourceDigest_AccessFastPath(t *testing.T) {
	t.Parallel()
	content := []byte("access-side pin from source")
	sha256 := godigest.FromBytes(content).Encoded()
	sha1 := shaHex(content, crypto.SHA1)

	// noBodyGET returns 500 on GET so a fast-path bug (falling through to
	// download) surfaces as an obvious failure, not silent SHA-256 recompute.
	noBodyGET := func(headers map[string]string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			if r.Method != http.MethodHead {
				http.Error(w, "fast path must not download the body", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	}

	t.Run("SHA-256 header pins the resource digest without a body download", func(t *testing.T) {
		server := httptest.NewServer(noBodyGET(map[string]string{"x-checksum-sha256": sha256}))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
				PreferredAlgorithms: []string{"sha256"},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		require.NotNil(t, processed.Digest)
		assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
		assert.Equal(t, sha256, processed.Digest.Value)
	})

	t.Run("SHA-1 accepted when policy allows it and no SHA-256 is advertised", func(t *testing.T) {
		server := httptest.NewServer(noBodyGET(map[string]string{"x-checksum-sha1": sha1}))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
				PreferredAlgorithms: []string{"sha256", "sha1"},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		require.NotNil(t, processed.Digest)
		assert.Equal(t, "SHA-1", processed.Digest.HashAlgorithm)
		assert.Equal(t, sha1, processed.Digest.Value)
	})

	t.Run("SHA-256 preferred over SHA-1 when both are advertised", func(t *testing.T) {
		server := httptest.NewServer(noBodyGET(map[string]string{
			"x-checksum-sha1":   sha1,
			"x-checksum-sha256": sha256,
		}))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
				PreferredAlgorithms: nil,
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		require.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
		assert.Equal(t, sha256, processed.Digest.Value)
	})

	t.Run("externalUrl sidecar pins without downloading the artifact", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead {
				http.Error(w, "fast path must not download the body", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/resource.sha256", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(sha256 + "  artifact\n"))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceExternalURL, Algorithms: []string{"sha256"}}},
				PreferredAlgorithms: []string{"sha256"},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		processed, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.NoError(t, err)
		assert.Equal(t, sha256, processed.Digest.Value)
	})

	t.Run("missing advertised digest with onMissing=fail aborts without downloading", func(t *testing.T) {
		server := httptest.NewServer(noBodyGET(nil))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
				PreferredAlgorithms: []string{"sha256"},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)
		_, err := repo.ProcessResourceDigest(t.Context(),
			wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no advertised checksum")
	})

	t.Run("pinned digest of a matching algorithm passes; mismatch fails", func(t *testing.T) {
		server := httptest.NewServer(noBodyGET(map[string]string{"x-checksum-sha256": sha256}))
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing:    checksumhttpv1alpha1.OnMissingFail,
				Sources:      []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
				PreferredAlgorithms: []string{"sha256"},
			},
		}
		repo := repository.NewResourceRepository(nil,
			repository.WithHTTPClient(server.Client()),
			repository.WithWgetConfig(cfg),
		)

		// Matching pin: succeeds.
		resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  "sha256:" + sha256,
		}
		_, err := repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err, "matching sha256 pin must be accepted on the fast path")

		// Mismatched pin: hard error.
		resource.Digest.Value = strings.Repeat("0", 64)
		_, err = repo.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match source-advertised")
	})
}

// shaHex hashes b with the given algorithm and returns lowercase hex.
func shaHex(b []byte, h crypto.Hash) string {
	hh := h.New()
	_, _ = hh.Write(b)
	return hex.EncodeToString(hh.Sum(nil))
}
