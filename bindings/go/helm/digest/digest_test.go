package digest_test

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/digest"
	"ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const testIndexYAML = `apiVersion: v1
entries:
  mychart:
    - urls:
        - https://example.com/charts/mychart-1.0.0.tgz
      name: mychart
      description: A test chart
      version: 1.0.0
      digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
      apiVersion: v2
    - urls:
        - https://example.com/charts/mychart-0.1.0.tgz
      name: mychart
      description: A test chart
      version: 0.1.0
      digest: "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"
      apiVersion: v2
  barehex:
    - urls:
        - https://example.com/charts/barehex-1.0.0.tgz
      name: barehex
      description: A chart with bare hex digest
      version: 1.0.0
      digest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
      apiVersion: v2
`

func helmAccessResource(t *testing.T, helmRepository, helmChart string) *descruntime.Resource {
	t.Helper()
	data := map[string]string{
		"type":           "helm/v1",
		"helmRepository": helmRepository,
		"helmChart":      helmChart,
	}
	raw, err := json.Marshal(data)
	require.NoError(t, err)

	r := &descruntime.Resource{}
	r.Name = "mychart"
	r.Version = "1.0.0"
	r.Type = "helmChart"
	r.Access = &runtime.Raw{
		Type: runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion),
		Data: raw,
	}
	return r
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	t.Parallel()
	p := digest.NewDigestProcessor("")
	scheme := p.GetResourceRepositoryScheme()
	require.NotNil(t, scheme)

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType("Helm", "v1"),
		runtime.NewUnversionedType("Helm"),
		runtime.NewVersionedType("helm", "v1"),
		runtime.NewUnversionedType("helm"),
	} {
		assert.True(t, scheme.IsRegistered(typ), "expected type %s to be registered", typ)
	}
}

func TestGetResourceDigestProcessorCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	p := digest.NewDigestProcessor("")

	t.Run("returns identity for HTTPS helm repository", func(t *testing.T) {
		t.Parallel()
		resource := helmAccessResource(t, "https://charts.example.com/stable", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "https", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
		assert.Equal(t, "stable", identity["path"])
	})

	t.Run("returns identity for HTTP helm repository with port", func(t *testing.T) {
		t.Parallel()
		resource := helmAccessResource(t, "http://charts.example.com:8080/repo", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "http", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
		assert.Equal(t, "8080", identity["port"])
		assert.Equal(t, "repo", identity["path"])
	})

	t.Run("returns nil for empty helm repository", func(t *testing.T) {
		t.Parallel()
		resource := helmAccessResource(t, "", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		t.Parallel()
		resource := &descruntime.Resource{}
		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.Error(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		t.Parallel()
		resource := helmAccessResource(t, "://invalid", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error parsing helm repository URL to identity")
	})

	t.Run("returns identity for OCI helm repository", func(t *testing.T) {
		t.Parallel()
		resource := helmAccessResource(t, "oci://registry.example.com/charts/mychart:1.0.0", "")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "OCIRegistry", identity["type"])
		assert.Equal(t, "oci", identity["scheme"])
		assert.Equal(t, "registry.example.com", identity["hostname"])
	})
}

func TestProcessResourceDigest_HTTP(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			w.Header().Set("Content-Type", "application/x-yaml")
			w.Write([]byte(testIndexYAML))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	t.Run("applies digest from index with sha256 prefix", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "mychart:1.0.0")

		result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Digest)

		assert.Equal(t, "SHA-256", result.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", result.Digest.NormalisationAlgorithm)
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result.Digest.Value)
	})

	t.Run("applies digest from index with bare hex", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "barehex:1.0.0")

		result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Digest)

		assert.Equal(t, "SHA-256", result.Digest.HashAlgorithm)
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result.Digest.Value)
	})

	t.Run("does not mutate original resource", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "mychart:1.0.0")

		result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Nil(t, resource.Digest, "original resource should not be mutated")
		assert.NotNil(t, result.Digest)
	})

	t.Run("verifies matching existing digest", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "mychart:1.0.0")
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}

		result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result.Digest.Value)
	})

	t.Run("returns error on digest mismatch", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "mychart:1.0.0")
		resource.Digest = &descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
		}

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "digest value mismatch")
	})

	t.Run("returns error when chart not found in index", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "nonexistent:1.0.0")

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in repository index")
	})

	t.Run("returns error for empty helm repository", func(t *testing.T) {
		t.Parallel()
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, "", "mychart:1.0.0")

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helm repository URL is required")
	})
}

// A chart the index lists without digest is downloaded and hashed. The HTTP configuration
// applies to the index and the chart: the server's certificate is only trusted through it.
func TestProcessResourceDigest_HTTPIndexWithoutDigest(t *testing.T) {
	tgz, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  mychart:\n    - urls: [%s/mychart-0.1.0.tgz]\n      name: mychart\n      version: 0.1.0\n      apiVersion: v2\n", srv.URL)
		case "/mychart-0.1.0.tgz":
			_, _ = w.Write(tgz)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	trusted := &httpv1alpha1.Config{TLSConfig: httpv1alpha1.TLSConfig{
		RootCAsPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})),
	}}

	for _, tc := range []struct {
		name    string
		chart   string
		opts    []digest.Option
		wantErr string
	}{
		{name: "with the HTTP configuration", chart: "mychart:0.1.0", opts: []digest.Option{digest.WithHTTPConfig(trusted)}},
		{name: "without a requested version, the version the index selects", chart: "mychart", opts: []digest.Option{digest.WithHTTPConfig(trusted)}},
		{name: "without the HTTP configuration", chart: "mychart:0.1.0", wantErr: "certificate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			result, err := digest.NewDigestProcessor(t.TempDir(), tc.opts...).ProcessResourceDigest(t.Context(), helmAccessResource(t, srv.URL, tc.chart), nil)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(godigest.FromBytes(tgz).Encoded(), result.Digest.Value)
			r.Equal("genericBlobDigest/v1", result.Digest.NormalisationAlgorithm)
		})
	}
}

// The chart the index selects is downloaded from the URL the index lists, resolved against
// the repository, without fetching the index again. Credentials go to the repository host only.
func TestProcessResourceDigest_HTTPIndexWithoutDigestDownloadsListedURL(t *testing.T) {
	tgz, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	creds := &runtime.Raw{
		Type: runtime.NewVersionedType("HelmHTTPCredentials", "v1"),
		Data: []byte(`{"type":"HelmHTTPCredentials/v1","username":"user","password":"secret"}`),
	}

	// elsewhere serves the chart on another host and records whether credentials reached it.
	var elsewhereAuth atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, _, ok := req.BasicAuth(); ok {
			elsewhereAuth.Store(true)
		}
		_, _ = w.Write(tgz)
	}))
	t.Cleanup(elsewhere.Close)

	for _, tc := range []struct {
		name      string
		chartURL  func(repo string) string
		wantAuth  bool
		elsewhere bool
	}{
		{name: "relative URL on the repository host", chartURL: func(string) string { return "charts/mychart-0.1.0.tgz" }, wantAuth: true},
		{name: "absolute URL on another host", chartURL: func(string) string { return elsewhere.URL + "/mychart-0.1.0.tgz" }, elsewhere: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			elsewhereAuth.Store(false)
			var indexFetches atomic.Int32
			var chartAuth atomic.Bool
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/index.yaml":
					indexFetches.Add(1)
					_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  mychart:\n    - urls: [%s]\n      name: mychart\n      version: 0.1.0\n      apiVersion: v2\n", tc.chartURL(srv.URL))
				case "/charts/mychart-0.1.0.tgz":
					_, _, ok := req.BasicAuth()
					chartAuth.Store(ok)
					_, _ = w.Write(tgz)
				default:
					http.NotFound(w, req)
				}
			}))
			t.Cleanup(srv.Close)

			result, err := digest.NewDigestProcessor(t.TempDir()).ProcessResourceDigest(t.Context(), helmAccessResource(t, srv.URL, "mychart:0.1.0"), creds)
			r.NoError(err)
			r.Equal(godigest.FromBytes(tgz).Encoded(), result.Digest.Value)
			r.Equal(int32(1), indexFetches.Load(), "the index is fetched once")
			if tc.elsewhere {
				r.False(elsewhereAuth.Load(), "credentials must not reach another host")
			} else {
				r.Equal(tc.wantAuth, chartAuth.Load())
			}
		})
	}
}

func TestProcessResourceDigest_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real repository test in short mode")
	}
	t.Parallel()

	p := digest.NewDigestProcessor(t.TempDir())
	resource := helmAccessResource(t, "https://grafana.github.io/helm-charts", "grafana:8.8.2")

	result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Digest)

	assert.Equal(t, "SHA-256", result.Digest.HashAlgorithm)
	assert.Equal(t, "genericBlobDigest/v1", result.Digest.NormalisationAlgorithm)
	assert.Equal(t, "b244a04ccfac950853080eede6bf1b716b2966ca707e69f6f3623b3d690e36b8", result.Digest.Value)
}
