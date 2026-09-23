package input_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/wget/input"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
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

// peekOrFail is the strictest reduced-config mode: verify against the
// source-advertised checksum and fail when none is available.
func peekOrFail() *checksumhttpv1alpha1.Config {
	return &checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModePeekWithHEADOrFail}
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

		// Disable central verification so only the provided digest is checked.
		method := &input.InputMethod{ChecksumConfig: &checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModeDisable}}
		result, err := method.ProcessResource(t.Context(),
			wgetResourceWithDigest(t, server.URL+"/artifact", hwSHA256), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("mismatching provided digest fails", func(t *testing.T) {
		server := serve(t)
		defer server.Close()

		method := &input.InputMethod{ChecksumConfig: &checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModeDisable}}
		_, err := method.ProcessResource(t.Context(),
			wgetResourceWithDigest(t, server.URL+"/artifact", strings.Repeat("0", 64)), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "digest mismatch")
	})

	t.Run("unsupported provided hash algorithm fails", func(t *testing.T) {
		server := serve(t)
		defer server.Close()

		r := wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"})
		r.Digest = &constructorruntime.Digest{HashAlgorithm: "SHA-1", Value: hwSHA1}
		method := &input.InputMethod{ChecksumConfig: &checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModeDisable}}
		_, err := method.ProcessResource(t.Context(), r, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported provided hash algorithm")
	})

	t.Run("provided digest and policy are both verified", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha1", hwSHA1)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		// PeekWithHEADOrFail verifies the bytes against the source-advertised
		// SHA-1 header, while the provided SHA-256 digest is verified
		// independently. Both must pass.
		method := &input.InputMethod{ChecksumConfig: peekOrFail()}
		result, err := method.ProcessResource(t.Context(),
			wgetResourceWithDigest(t, server.URL+"/artifact", hwSHA256), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})
}

// TestProcessResource_ChecksumPolicy_HeaderVerification confirms that the
// reduced, header-only checksum configuration verifies a downloaded blob
// against the source-advertised x-checksum-sha256 header, and that OCM
// credentials still reach the artifact request. No sibling checksum URL is
// fetched, so credential leakage to a checksum host is structurally
// impossible.
func TestProcessResource_ChecksumPolicy_HeaderVerification(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")
	creds := &credv1.WgetCredentials{
		Type:          credv1.WgetCredentialsVersionedType,
		IdentityToken: "my-token",
	}
	insecure := true
	httpConfig := &httpv1alpha1.Config{
		TLSConfig: httpv1alpha1.TLSConfig{
			InsecureSkipVerify: &insecure,
		},
	}

	t.Run("advertised header verifies and credentials reach the artifact", func(t *testing.T) {
		var artifactAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			artifactAuth = r.Header.Get("Authorization")
			w.Header().Set("x-checksum-sha256", hwSHA256)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		method := &input.InputMethod{HTTPConfig: httpConfig, ChecksumConfig: peekOrFail()}
		result, err := method.ProcessResource(t.Context(),
			wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"}), creds)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
		assert.Equal(t, "Bearer my-token", artifactAuth, "artifact fetch must carry credentials")
	})

	t.Run("tampered advertised header fails construction", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha256", strings.Repeat("f", 64))
			_, _ = w.Write(content)
		}))
		defer server.Close()

		method := &input.InputMethod{ChecksumConfig: peekOrFail()}
		_, err := method.ProcessResource(t.Context(),
			wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}

// TestProcessResource_ChecksumPolicy_ConfigDriven confirms that the shared
// checksum.http.config.ocm.software config is honored on the input side:
//
//   - the top-level mode is applied to every wget input,
//   - a host-scoped override wins over the default.
func TestProcessResource_ChecksumPolicy_ConfigDriven(t *testing.T) {
	t.Parallel()
	content := []byte("hello world")

	// Handler that advertises the correct SHA-256 via the x-checksum-sha256
	// header (Artifactory-style) and, on the ".sha256" sibling, serves that
	// same digest so both httpHeader and externalUrl sources can match.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
			return
		}
		w.Header().Set("x-checksum-sha256", hwSHA256)
		_, _ = w.Write(content)
	})

	t.Run("default mode applies to the resource", func(t *testing.T) {
		server := httptest.NewServer(handler)
		defer server.Close()

		method := &input.InputMethod{ChecksumConfig: peekOrFail()}
		result, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("host override wins over default", func(t *testing.T) {
		// Default mode "Compute" accepts any bytes without verification; the
		// host override for the artifact's actual host is "PeekWithHEADOrFail".
		// A tampered advertised digest therefore fails against the override but
		// would go unnoticed under the default.
		badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-checksum-sha256", strings.Repeat("f", 64))
			_, _ = w.Write(content)
		}))
		defer badServer.Close()
		host, _, _ := strings.Cut(strings.TrimPrefix(badServer.URL, "http://"), "/")

		cfg := &checksumhttpv1alpha1.Config{
			Mode: checksumhttpv1alpha1.ChecksumModeCompute,
			Hosts: map[string]*checksumhttpv1alpha1.ChecksumPolicy{
				host: {Mode: checksumhttpv1alpha1.ChecksumModePeekWithHEADOrFail},
			},
		}
		method := &input.InputMethod{ChecksumConfig: cfg}
		_, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": badServer.URL + "/artifact",
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}
