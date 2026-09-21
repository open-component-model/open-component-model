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
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/wget/input"
	wgetconfigv1alpha1 "ocm.software/open-component-model/bindings/go/wget/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
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

// TestProcessResource_ChecksumPolicy_CELURL exercises the CEL-driven URL field
// on an externalUrl source: a custom expression pointing at a non-sibling
// checksum location, and rejection of unwrapped / non-string expressions.
func TestProcessResource_ChecksumPolicy_CELURL(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")

	t.Run("custom CEL url expression is honored per algorithm", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only the SHA-256 sibling exists, hosted at a non-default path.
			if r.URL.Path == "/checksums/sha256/artifact" {
				_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
				return
			}
			if strings.HasSuffix(r.URL.Path, "/artifact") {
				_, _ = w.Write(content)
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{
					"type":       "externalUrl",
					"algorithms": []any{"sha256"},
					// resource.url.scheme/host/path expose the parsed artifact URL.
					"url": `${resource.url.scheme + "://" + resource.url.host + "/checksums/" + ext + resource.url.path}`,
				},
			}},
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})

	t.Run("unwrapped url expression is rejected at policy build time", func(t *testing.T) {
		_, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": "http://example.invalid/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{"type": "externalUrl", "url": `"https://example.invalid/foo"`},
			}},
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "single CEL expression wrapped in ${...}")
	})

	t.Run("url expression that does not evaluate to a string is rejected", func(t *testing.T) {
		_, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": "http://example.invalid/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{"type": "externalUrl", "url": `${42}`},
			}},
		}), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must evaluate to a string")
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

// TestProcessResource_ChecksumPolicy_CredentialsForwarded confirms three
// invariants on how OCM credentials propagate to the sibling checksum fetch:
//
//   - Same-origin HTTPS: credentials are forwarded so authenticated Maven mirrors
//     and registries behind a bearer token/basic auth verify checksums end-to-end.
//   - Cross-origin (any scheme): credentials are stripped. The checksum URL comes
//     from a user-controlled CEL expression; sending Authorization to arbitrary
//     hosts would leak credentials to attacker-controlled infrastructure.
//   - Plain HTTP (even same host): credentials are stripped. Non-TLS transport
//     leaks the header on the wire.
//
// Guards CWE-200 (sensitive-data exposure) surfaced during PR review.
func TestProcessResource_ChecksumPolicy_CredentialsForwarded(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")

	// Standalone mirror used for the cross-origin cases. TLS + separate host so
	// the artifact server's origin never matches.
	var mirrorAuth string
	mirror := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(hwSHA256 + "  artifact\n"))
	}))
	defer mirror.Close()

	// Also a plain-HTTP mirror on the same host as the artifact to prove the
	// scheme downgrade drops credentials even when the host would otherwise
	// match. Reuses the same handler shape.
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
	// InsecureSkipVerify lets the input method's HTTP client trust the httptest
	// server's self-signed certificate. Without this the download itself would
	// fail before verifyChecksum is reached.
	httpConfig := &httpv1alpha1.Config{
		TLSConfig: httpv1alpha1.TLSConfig{
			InsecureSkipVerify: &insecure,
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

		resource := wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{"type": "externalUrl", "algorithms": []any{"sha256"}},
			}},
		})
		result, err := (&input.InputMethod{HTTPConfig: httpConfig}).ProcessResource(t.Context(), resource, creds)
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

		// CEL expression points the checksum URL at the mirror on a different
		// host, exercising the same-origin filter.
		resource := wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{
					"type":       "externalUrl",
					"algorithms": []any{"sha256"},
					"url":        fmt.Sprintf(`${"%s/artifact.sha256"}`, mirror.URL),
				},
			}},
		})
		result, err := (&input.InputMethod{HTTPConfig: httpConfig}).ProcessResource(t.Context(), resource, creds)
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

		// Point the checksum URL at the plain-HTTP mirror. Different scheme (and
		// implicitly different port) so the same-origin filter rejects it even
		// though hostname is 127.0.0.1 on both.
		resource := wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{"sources": []any{
				map[string]any{
					"type":       "externalUrl",
					"algorithms": []any{"sha256"},
					"url":        fmt.Sprintf(`${"%s/artifact.sha256"}`, httpMirror.URL),
				},
			}},
		})
		result, err := (&input.InputMethod{HTTPConfig: httpConfig}).ProcessResource(t.Context(), resource, creds)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
		assert.Equal(t, "Bearer my-token", artifactAuth, "artifact fetch still carries credentials")
		assert.Empty(t, httpMirrorAuth, "plain-HTTP checksum fetch must NOT carry credentials even for the same host")
	})
}

// TestProcessResource_ChecksumPolicy_ConfigDriven confirms that the shared
// wget.config.ocm.software config is honored on the input side:
//
//   - a defaultChecksumPolicy applies to a resource whose spec omits its own,
//   - a host-scoped override wins over the default,
//   - a spec-level policy on the resource wins over both.
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

	t.Run("default policy applies when the spec has none", func(t *testing.T) {
		server := httptest.NewServer(handler)
		defer server.Close()

		cfg := &wgetconfigv1alpha1.Config{
			DefaultChecksumPolicy: &v1.ChecksumPolicy{
				OnMissing: v1.OnMissingFail,
				Sources:   []v1.ChecksumSource{{Type: v1.ChecksumSourceHTTPHeader}},
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

		cfg := &wgetconfigv1alpha1.Config{
			DefaultChecksumPolicy: &v1.ChecksumPolicy{
				OnMissing: v1.OnMissingCompute,
				Sources:   []v1.ChecksumSource{{Type: v1.ChecksumSourceHTTPHeader}},
			},
			Hosts: map[string]*wgetconfigv1alpha1.HostConfig{
				host: {
					ChecksumPolicy: &v1.ChecksumPolicy{
						OnMissing: v1.OnMissingFail,
						Sources:   []v1.ChecksumSource{{Type: v1.ChecksumSourceHTTPHeader}},
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

	t.Run("spec policy wins over config", func(t *testing.T) {
		server := httptest.NewServer(handler)
		defer server.Close()

		// Config says fail-on-missing with an externalUrl source that would
		// need a sibling. Spec says: use httpHeader instead. Spec wins, so
		// the httpHeader source verifies the download and construction
		// succeeds.
		cfg := &wgetconfigv1alpha1.Config{
			DefaultChecksumPolicy: &v1.ChecksumPolicy{
				OnMissing: v1.OnMissingFail,
				Sources:   []v1.ChecksumSource{{Type: v1.ChecksumSourceExternalURL, Algorithms: []string{"md5"}}},
			},
		}
		method := &input.InputMethod{WgetConfig: cfg}
		result, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{
			"url": server.URL + "/artifact",
			"checksumPolicy": map[string]any{
				"onMissing": "fail",
				"sources":   []any{map[string]any{"type": "httpHeader"}},
			},
		}), nil)
		require.NoError(t, err)
		assert.Equal(t, "sha256:"+hwSHA256, blobDigest(t, result.ProcessedBlobData))
	})
}
