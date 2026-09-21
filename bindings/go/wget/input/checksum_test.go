package input_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/wget/input"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
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

// TestProcessResource_ChecksumPolicy_CredentialsForwarded confirms three
// invariants on how OCM credentials propagate to the sibling checksum fetch
// when a checksum.http.config.ocm.software policy is configured:
//
//   - Same-origin HTTPS: credentials are forwarded so authenticated Maven mirrors
//     and registries behind a bearer token/basic auth verify checksums end-to-end.
//   - Cross-origin (any scheme): credentials are stripped. The checksum URL is
//     user-configured; sending Authorization to arbitrary hosts would leak
//     credentials to attacker-controlled infrastructure.
//   - Plain HTTP (even same host): credentials are stripped. Non-TLS transport
//     leaks the header on the wire.
//
// Guards CWE-200 (sensitive-data exposure) surfaced during PR review.
func TestProcessResource_ChecksumPolicy_CredentialsForwarded(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")

	var mirrorAuth string
	mirror := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
	}))
	defer mirror.Close()

	var httpMirrorAuth string
	httpMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpMirrorAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
	}))
	defer httpMirror.Close()

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
	// externalUrl policy with the default sibling URL — credentials only reach
	// the sibling fetch when it lands on the artifact's same origin.
	externalPolicy := &checksumhttpv1alpha1.Config{
		DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
			OnMissing: checksumhttpv1alpha1.OnMissingFail,
			Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceExternalURL, Algorithms: []string{"sha256"}}},
		},
	}

	t.Run("same-origin HTTPS forwards credentials to checksum fetch", func(t *testing.T) {
		var (
			artifactAuth string
			checksumAuth string
		)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, ".sha256"):
				checksumAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
			default:
				artifactAuth = r.Header.Get("Authorization")
				_, _ = w.Write(content)
			}
		}))
		defer server.Close()

		method := &input.InputMethod{HTTPConfig: httpConfig, WgetConfig: externalPolicy}
		result, err := method.ProcessResource(t.Context(),
			wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"}), creds)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
		assert.Equal(t, "Bearer my-token", artifactAuth, "artifact fetch must carry credentials")
		assert.Equal(t, "Bearer my-token", checksumAuth, "same-origin HTTPS checksum fetch must reuse credentials")
	})

	t.Run("cross-origin HTTPS strips credentials from checksum fetch", func(t *testing.T) {
		mirrorAuth = ""
		var artifactAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			artifactAuth = r.Header.Get("Authorization")
			_, _ = w.Write(content)
		}))
		defer server.Close()

		// Point the checksum URL at the mirror on a different host via a
		// per-host config override.
		crossOriginPolicy := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingFail,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceExternalURL, URL: mirror.URL + "/artifact.sha256", Algorithms: []string{"sha256"}}},
			},
		}
		method := &input.InputMethod{HTTPConfig: httpConfig, WgetConfig: crossOriginPolicy}
		result, err := method.ProcessResource(t.Context(),
			wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"}), creds)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
		assert.Equal(t, "Bearer my-token", artifactAuth, "artifact fetch still carries credentials")
		assert.Empty(t, mirrorAuth, "cross-origin checksum fetch must NOT carry credentials")
	})

	t.Run("same-host plain-HTTP strips credentials from checksum fetch", func(t *testing.T) {
		httpMirrorAuth = ""
		var artifactAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			artifactAuth = r.Header.Get("Authorization")
			_, _ = w.Write(content)
		}))
		defer server.Close()

		httpMirrorPolicy := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingFail,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceExternalURL, URL: httpMirror.URL + "/artifact.sha256", Algorithms: []string{"sha256"}}},
			},
		}
		method := &input.InputMethod{HTTPConfig: httpConfig, WgetConfig: httpMirrorPolicy}
		result, err := method.ProcessResource(t.Context(),
			wgetInputResource(t, map[string]any{"url": server.URL + "/artifact"}), creds)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
		assert.Equal(t, "Bearer my-token", artifactAuth, "artifact fetch still carries credentials")
		assert.Empty(t, httpMirrorAuth, "plain-HTTP checksum fetch must NOT carry credentials even for the same host")
	})
}

// TestProcessResource_ChecksumPolicy_ConfigDriven confirms that the shared
// checksum.http.config.ocm.software config is honored on the input side:
//
//   - a defaultChecksumPolicy is applied to every wget input,
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

	t.Run("default policy applies to the resource", func(t *testing.T) {
		server := httptest.NewServer(handler)
		defer server.Close()

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingFail,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
			},
		}
		method := &input.InputMethod{WgetConfig: cfg}
		result, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("host override wins over default", func(t *testing.T) {
		// Two servers on different ports: default policy says "compute"
		// (any bytes accepted), host override for the artifact's actual host
		// says "fail". A tampered response should therefore fail against the
		// host override but would silently succeed under the default.
		badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Advertise a mismatching digest that would fail a fail-policy but
			// go unnoticed by compute-on-missing.
			w.Header().Set("x-checksum-sha256", strings.Repeat("f", 64))
			_, _ = w.Write(content)
		}))
		defer badServer.Close()
		host, _, _ := strings.Cut(strings.TrimPrefix(badServer.URL, "http://"), "/")

		cfg := &checksumhttpv1alpha1.Config{
			DefaultChecksumPolicy: &checksumhttpv1alpha1.ChecksumPolicy{
				OnMissing: checksumhttpv1alpha1.OnMissingCompute,
				Sources:   []checksumhttpv1alpha1.ChecksumSource{{Type: checksumhttpv1alpha1.ChecksumSourceHTTPHeader}},
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
		method := &input.InputMethod{WgetConfig: cfg}
		_, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": badServer.URL + "/artifact",
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}
