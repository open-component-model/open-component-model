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
	"ocm.software/open-component-model/bindings/go/pypi/digest"
	"ocm.software/open-component-model/bindings/go/pypi/repository/resource"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const wheelName = "requests-2.32.3-py3-none-any.whl"

func pypiResource(indexURL, version string) *descriptor.Resource {
	return &descriptor.Resource{
		Access: &v1alpha1.PyPI{
			Type:     runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version),
			IndexURL: indexURL,
			Project:  "requests",
			Version:  version,
		},
	}
}

// index serves a project page with a single wheel and the wheel bytes.
func index(t *testing.T) *httptest.Server {
	t.Helper()
	page := `{"meta":{"api-version":"1.1"},"name":"requests","files":[
	  {"filename":"` + wheelName + `","url":"/files/` + wheelName + `","hashes":{"sha256":"abc"}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/simple/requests/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(page))
		case "/files/" + wheelName:
			_, _ = w.Write([]byte("WHEELDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sha256OfArchive hashes the whole tgz the resource repository returns for res.
func sha256OfArchive(t *testing.T, srv *httptest.Server, res *descriptor.Resource) string {
	t.Helper()
	b, err := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client())).DownloadResource(context.Background(), res, nil)
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
	srv := index(t)
	p := digest.NewDigestProcessor(resource.WithHTTPClient(srv.Client()))
	want := sha256OfArchive(t, srv, pypiResource(srv.URL+"/simple", "2.32.3"))

	t.Run("applies sha256 generic blob digest over the tgz", func(t *testing.T) {
		res, err := p.ProcessResourceDigest(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
		require.NoError(t, err)
		require.NotNil(t, res.Digest)
		assert.Equal(t, "SHA-256", res.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
		assert.Equal(t, want, res.Digest.Value)
		assert.NotEqual(t, fmt.Sprintf("%x", sha256.Sum256([]byte("WHEELDATA"))), res.Digest.Value, "digest must cover the archive, not the raw wheel")
	})

	t.Run("is stable across downloads", func(t *testing.T) {
		first, err := p.ProcessResourceDigest(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
		require.NoError(t, err)
		second, err := p.ProcessResourceDigest(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
		require.NoError(t, err)
		assert.Equal(t, first.Digest.Value, second.Digest.Value)
	})

	t.Run("does not modify the input resource", func(t *testing.T) {
		in := pypiResource(srv.URL+"/simple", "2.32.3")
		_, err := p.ProcessResourceDigest(context.Background(), in, nil)
		require.NoError(t, err)
		assert.Nil(t, in.Digest)
	})

	t.Run("verifies a matching pre-set digest and fills in the algorithms", func(t *testing.T) {
		res := pypiResource(srv.URL+"/simple", "2.32.3")
		res.Digest = &descriptor.Digest{Value: want}
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, "SHA-256", out.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", out.Digest.NormalisationAlgorithm)
	})

	t.Run("accepts other spellings of the algorithms", func(t *testing.T) {
		res := pypiResource(srv.URL+"/simple", "2.32.3")
		res.Digest = &descriptor.Digest{HashAlgorithm: "sha-256", NormalisationAlgorithm: "GENERICBLOBDIGEST/V1", Value: want}
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, "SHA-256", out.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", out.Digest.NormalisationAlgorithm)
	})

	t.Run("rejects a mismatched pre-set digest", func(t *testing.T) {
		res := pypiResource(srv.URL+"/simple", "2.32.3")
		res.Digest = &descriptor.Digest{Value: "deadbeef"}
		_, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.ErrorContains(t, err, "digest value mismatch")
	})

	t.Run("rejects a different hash algorithm", func(t *testing.T) {
		res := pypiResource(srv.URL+"/simple", "2.32.3")
		res.Digest = &descriptor.Digest{HashAlgorithm: "SHA-512", Value: want}
		_, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.ErrorContains(t, err, "hash algorithm mismatch")
	})

	t.Run("leaves a resource excluded from the signature alone", func(t *testing.T) {
		res := pypiResource("http://127.0.0.1:1/simple", "2.32.3") // nothing listens here: the exclusion must skip the download
		res.Digest = descriptor.NewExcludeFromSignatureDigest()
		out, err := p.ProcessResourceDigest(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, descriptor.NewExcludeFromSignatureDigest(), out.Digest)
	})

	t.Run("a failed download is an error", func(t *testing.T) {
		_, err := p.ProcessResourceDigest(context.Background(), pypiResource(srv.URL+"/simple", "9.9.9"), nil)
		require.ErrorContains(t, err, "error downloading pypi distribution for digest")
	})

	t.Run("credential consumer identity is PyPIRepository", func(t *testing.T) {
		id, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), pypiResource("https://pypi.org/simple", "2.32.3"))
		require.NoError(t, err)
		assert.Equal(t, "PyPIRepository", id[runtime.IdentityAttributeType])
		assert.Equal(t, "pypi.org", id[runtime.IdentityAttributeHostname])
	})

	t.Run("identity of a resource without access is an error", func(t *testing.T) {
		_, err := p.GetResourceDigestProcessorCredentialConsumerIdentity(context.Background(), nil)
		require.ErrorContains(t, err, "access is required")
	})
}
