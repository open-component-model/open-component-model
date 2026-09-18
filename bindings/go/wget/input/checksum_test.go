package input_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/wget/input"
)

// Digests of the ASCII payload "hello world".
const (
	hwSHA256    = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	hwSHA1      = "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"
	hwSHA256B64 = "uU0nuZNNPgilLlLX2n2r+sSE7+N6U4DukIj3rOLvzek="
)

// blobDigest reads the (possibly precalculated) digest of the processed blob.
func blobDigest(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	da, ok := b.(blob.DigestAware)
	require.True(t, ok, "processed blob must be digest-aware")
	digest, known := da.Digest()
	require.True(t, known, "processed blob must expose a digest")
	return digest
}

func TestProcessResource_ChecksumPolicy(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")

	t.Run("verifies against RFC 9530 Content-Digest header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Digest", fmt.Sprintf("sha-256=:%s:", hwSHA256B64))
			_, _ = w.Write(content)
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url":            server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{map[string]any{"type": "httpHeader"}}},
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, content, readBlob(t, result.ProcessedBlobData))
		// The stored digest is always the canonical SHA-256, in go-digest form.
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("verifies against x-checksum-sha1 and still stores sha256", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha1", hwSHA1)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url":            server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{map[string]any{"type": "httpHeader"}}},
		}), nil)
		require.NoError(t, err)
		// Verified via SHA-1, but the OCM resource digest is SHA-256 (no OCI mismatch).
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("verifies against external sibling checksum URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, ".sha256"):
				_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
			case strings.HasSuffix(r.URL.Path, ".sha1"):
				http.NotFound(w, r)
			default:
				_, _ = w.Write(content)
			}
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{"type": "externalUrl", "algorithms": []any{"sha256", "sha1"}},
			}},
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("fails on transport checksum mismatch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha256", strings.Repeat("0", 64)) // valid length, wrong value
			_, _ = w.Write(content)
		}))
		defer server.Close()

		_, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url":            server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{map[string]any{"type": "httpHeader"}}},
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})

	t.Run("fails when no checksum available and onMissing defaults to fail", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(content) // no checksum headers
		}))
		defer server.Close()

		_, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url":            server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{map[string]any{"type": "httpHeader"}}},
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no checksum could be obtained")
	})

	t.Run("onMissing compute falls back to stream digest", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(content) // no checksum headers
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{
				"onMissing": "compute",
				"sources":   []any{map[string]any{"type": "httpHeader"}},
			},
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("no policy computes digest without verification", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(content)
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
		}), nil)
		require.NoError(t, err)
		// Without a policy the blob still yields its SHA-256 (computed lazily).
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})
}

// wgetResourceWithDigest builds a wget input resource carrying a provided digest.
func wgetResourceWithDigest(t *testing.T, url, value string) *constructorruntime.Resource {
	t.Helper()
	r := wgetInputResource(t, map[string]any{"url": url})
	r.Digest = &constructorruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  value,
	}
	return r
}

func TestProcessResource_ProvidedDigest(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")
	serve := func(t *testing.T) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(content)
		}))
	}

	t.Run("matching provided digest is verified and stored", func(t *testing.T) {
		server := serve(t)
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(),
			wgetResourceWithDigest(t, server.URL+"/artifact", hwSHA256), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("mismatching provided digest fails", func(t *testing.T) {
		server := serve(t)
		defer server.Close()

		_, err := (&input.InputMethod{}).ProcessResource(t.Context(),
			wgetResourceWithDigest(t, server.URL+"/artifact", strings.Repeat("0", 64)), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "digest mismatch")
	})

	t.Run("unsupported provided hash algorithm fails", func(t *testing.T) {
		server := serve(t)
		defer server.Close()

		r := wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"})
		r.Digest = &constructorruntime.Digest{HashAlgorithm: "SHA-1", Value: hwSHA1}
		_, err := (&input.InputMethod{}).ProcessResource(t.Context(), r, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported provided hash algorithm")
	})

	t.Run("provided digest and policy are both verified", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha1", hwSHA1)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		r := wgetResourceWithDigest(t, server.URL+"/artifact", hwSHA256)
		r.Input = wgetInputResource(t, map[string]any{
			"url":            server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{map[string]any{"type": "httpHeader"}}},
		}).Input
		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), r, nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})
}
