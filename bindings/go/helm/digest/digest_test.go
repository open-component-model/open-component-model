package digest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/digest"
	"ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
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
  nodigest:
    - urls:
        - https://example.com/charts/nodigest-1.0.0.tgz
      name: nodigest
      description: A chart without digest
      version: 1.0.0
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
		resource := helmAccessResource(t, "", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		assert.NoError(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for nil access", func(t *testing.T) {
		resource := &descruntime.Resource{}
		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		resource := helmAccessResource(t, "://invalid", "mychart:1.0.0")

		identity, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), resource)
		assert.Error(t, err)
		assert.Nil(t, identity)
		assert.Contains(t, err.Error(), "error parsing helm repository URL to identity")
	})

	t.Run("returns identity for OCI helm repository", func(t *testing.T) {
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
	defer srv.Close()

	t.Run("applies digest from index with sha256 prefix", func(t *testing.T) {
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
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "mychart:1.0.0")

		result, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Nil(t, resource.Digest, "original resource should not be mutated")
		assert.NotNil(t, result.Digest)
	})

	t.Run("verifies matching existing digest", func(t *testing.T) {
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

	t.Run("returns error when chart has no digest in index", func(t *testing.T) {
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "nodigest:1.0.0")

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no digest")
	})

	t.Run("returns error when chart not found in index", func(t *testing.T) {
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, srv.URL, "nonexistent:1.0.0")

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in repository index")
	})

	t.Run("returns error for empty helm repository", func(t *testing.T) {
		p := digest.NewDigestProcessor(t.TempDir())
		resource := helmAccessResource(t, "", "mychart:1.0.0")

		_, err := p.ProcessResourceDigest(t.Context(), resource, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helm repository URL is required")
	})
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
