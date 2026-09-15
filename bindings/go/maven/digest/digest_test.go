package digest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/digest"
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func mavenResource(repoURL, version string) *descriptor.Resource {
	return &descriptor.Resource{
		Access: &v2alpha1.Maven{
			Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
			RepoURL:    repoURL,
			GroupID:    "com.example",
			ArtifactID: "lib",
			Version:    version,
			Artifacts:  []v2alpha1.Artifact{{Extension: "jar"}},
		},
	}
}

// sha256Of hashes the whole content of the blob the resource repository
// returns for res. The digest processor's contract is to hash exactly that
// blob, so this is the value it must produce.
func sha256Of(t *testing.T, res *descriptor.Resource) string {
	t.Helper()
	b, err := resource.NewResourceRepository().DownloadResource(context.Background(), res, nil)
	require.NoError(t, err)
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	h := sha256.New()
	_, err = io.Copy(h, rc)
	require.NoError(t, err)
	return fmt.Sprintf("%x", h.Sum(nil))
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

	p := digest.NewDigestProcessor(resource.WithHTTPClient(srv.Client()))
	want := sha256Of(t, mavenResource(srv.URL, "1.2.3"))

	t.Run("applies sha256 generic blob digest over the tgz", func(t *testing.T) {
		res, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL, "1.2.3"), nil)
		require.NoError(t, err)
		require.NotNil(t, res.Digest)
		assert.Equal(t, "SHA-256", res.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
		assert.Equal(t, want, res.Digest.Value)
		assert.NotEqual(t, fmt.Sprintf("%x", sha256.Sum256(body)), res.Digest.Value, "digest must cover the archive, not the raw jar")
	})

	t.Run("is stable across downloads", func(t *testing.T) {
		first, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL, "1.2.3"), nil)
		require.NoError(t, err)
		second, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL, "1.2.3"), nil)
		require.NoError(t, err)
		assert.Equal(t, first.Digest.Value, second.Digest.Value)
	})

	t.Run("does not modify the input resource", func(t *testing.T) {
		in := mavenResource(srv.URL, "1.2.3")
		_, err := p.ProcessResourceDigest(context.Background(), in, nil)
		require.NoError(t, err)
		assert.Nil(t, in.Digest)
	})

	t.Run("verifies a matching pre-set digest and fills in the algorithms", func(t *testing.T) {
		res := mavenResource(srv.URL, "1.2.3")
		res.Digest = &descriptor.Digest{Value: want}
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, "SHA-256", out.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", out.Digest.NormalisationAlgorithm)
	})

	t.Run("accepts other spellings of the algorithms", func(t *testing.T) {
		res := mavenResource(srv.URL, "1.2.3")
		res.Digest = &descriptor.Digest{HashAlgorithm: "sha-256", NormalisationAlgorithm: "GENERICBLOBDIGEST/V1", Value: want}
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, "SHA-256", out.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", out.Digest.NormalisationAlgorithm)
	})

	t.Run("rejects a mismatched pre-set digest", func(t *testing.T) {
		res := mavenResource(srv.URL, "1.2.3")
		res.Digest = &descriptor.Digest{Value: "deadbeef"}
		_, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.ErrorContains(t, err, "digest value mismatch")
	})

	t.Run("rejects a different hash algorithm", func(t *testing.T) {
		res := mavenResource(srv.URL, "1.2.3")
		res.Digest = &descriptor.Digest{HashAlgorithm: "SHA-512", Value: want}
		_, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.ErrorContains(t, err, "hash algorithm mismatch")
	})

	t.Run("leaves a resource excluded from the signature alone", func(t *testing.T) {
		res := mavenResource("http://127.0.0.1:1", "1.2.3") // nothing listens here: the exclusion must skip the download
		res.Digest = descriptor.NewExcludeFromSignatureDigest()
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, descriptor.NewExcludeFromSignatureDigest(), out.Digest)
	})

	t.Run("rejects versions that cannot be pinned", func(t *testing.T) {
		for _, version := range []string{"LATEST", "RELEASE", "1.0-SNAPSHOT"} {
			_, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL, version), nil)
			require.ErrorContains(t, err, version)
			require.ErrorContains(t, err, "pin")
		}
	})

	t.Run("a failed download is an error", func(t *testing.T) {
		_, err := p.ProcessResourceDigest(context.Background(), mavenResource(srv.URL, "9.9.9"), nil)
		require.ErrorContains(t, err, "error downloading maven artifact for digest")
		require.ErrorContains(t, err, "404")
	})

	t.Run("credential consumer identity is MavenRepository", func(t *testing.T) {
		id, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), mavenResource("https://maven.example.com/repo", "1.2.3"))
		require.NoError(t, err)
		assert.Equal(t, "MavenRepository", id[runtime.IdentityAttributeType])
		assert.Equal(t, "maven.example.com", id[runtime.IdentityAttributeHostname])
	})

	t.Run("identity of a resource without access is an error", func(t *testing.T) {
		_, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), nil)
		require.ErrorContains(t, err, "access is required")
		_, err = p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), &descriptor.Resource{Access: &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte(`{"type":"Unknown/v1"}`)}})
		require.ErrorContains(t, err, "error converting access")
	})
}
