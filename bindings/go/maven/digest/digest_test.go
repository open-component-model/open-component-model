package digest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/digest"
	mavenv1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func mavenResource(repoURL string) *descriptor.Resource {
	return &descriptor.Resource{
		Access: &mavenv1.Maven{
			Type:       runtime.NewVersionedType(mavenv1.Type, mavenv1.Version),
			RepoURL:    repoURL,
			GroupID:    "com.example",
			ArtifactID: "lib",
			Version:    "1.2.3",
		},
	}
}

func TestProcessResourceDigest(t *testing.T) {
	body := []byte("the-artifact-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	p := digest.NewDigestProcessor(/* default client; httptest server is plain HTTP on loopback */)

	t.Run("applies sha256 generic blob digest", func(t *testing.T) {
		res, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL), nil)
		require.NoError(t, err)
		require.NotNil(t, res.Digest)
		assert.Equal(t, "SHA-256", res.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
		assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(body)), res.Digest.Value)
	})

	t.Run("verifies a matching pre-set digest", func(t *testing.T) {
		res := mavenResource(srv.URL)
		res.Digest = &descriptor.Digest{Value: fmt.Sprintf("%x", sha256.Sum256(body))}
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, "SHA-256", out.Digest.HashAlgorithm)
	})

	t.Run("rejects a mismatched pre-set digest", func(t *testing.T) {
		res := mavenResource(srv.URL)
		res.Digest = &descriptor.Digest{Value: "deadbeef"}
		_, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.ErrorContains(t, err, "digest mismatch")
	})

	t.Run("credential consumer identity is MavenRepository", func(t *testing.T) {
		id, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), mavenResource("https://maven.example.com/repo"))
		require.NoError(t, err)
		assert.Equal(t, "MavenRepository", id[runtime.IdentityAttributeType])
	})
}
