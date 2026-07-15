package digest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	testCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
	payload    = "abc"
)

// payloadDigest is the generic blob digest the processor produces: sha256 over
// the exact bytes served, matching old OCM. The processor never untars or
// decompresses the download, so any bytes stand in for a real archive.
var payloadDigest = godigest.FromString(payload).Encoded()

// mockGitHub emulates the GitHub archive API against the processor's real
// download path. Refs resolve to resolvedCommit; the tarball for testCommit
// serves payload. If resolveCalls is non-nil it counts ref resolutions. It
// returns the repo URL to point a resource at.
func mockGitHub(t *testing.T, resolvedCommit string, resolveCalls *int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/commits/main"):
			if resolveCalls != nil {
				*resolveCalls++
			}
			_, _ = w.Write([]byte(resolvedCommit))
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+testCommit):
			http.Redirect(w, r, "http://"+r.Host+"/codeload", http.StatusFound)
		case r.URL.Path == "/codeload":
			_, _ = w.Write([]byte(payload))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL + "/octocat/Hello-World"
}

func githubResource(repoURL, commit string) *descriptor.Resource {
	return githubResourceRef(repoURL, "", commit)
}

func githubResourceRef(repoURL, ref, commit string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
		},
		Access: &v1.GitHub{
			Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
			RepoURL: repoURL,
			Ref:     ref,
			Commit:  commit,
		},
	}
}

func TestDigestProcessor(t *testing.T) {
	processor := NewDigestProcessor()

	t.Run("ProcessResourceDigest", func(t *testing.T) {
		repoURL := mockGitHub(t, testCommit, nil)

		t.Run("applies the generic blob digest of the downloaded archive", func(t *testing.T) {
			res := githubResource(repoURL, testCommit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			require.NotNil(t, processed.Digest)
			assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
			assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
			assert.Equal(t, payloadDigest, processed.Digest.Value)

			assert.Nil(t, res.Digest, "input resource must not be mutated")
		})

		t.Run("verifies a matching pre-set digest", func(t *testing.T) {
			res := githubResource(repoURL, testCommit)
			res.Digest = &descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  payloadDigest,
			}
			_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)
		})

		t.Run("rejects a pre-set digest that does not match", func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				digest descriptor.Digest
				expect string
			}{
				{
					name:   "hash algorithm",
					digest: descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "genericBlobDigest/v1", Value: payloadDigest},
					expect: "hash algorithm mismatch",
				},
				{
					// An old-OCM descriptor may carry a correct value under a
					// normalisation algorithm this processor does not produce.
					name:   "normalisation algorithm",
					digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: payloadDigest},
					expect: "normalisation algorithm mismatch",
				},
				{
					name:   "value",
					digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: strings.Repeat("0", 64)},
					expect: "digest value mismatch",
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					res := githubResource(repoURL, testCommit)
					res.Digest = &tc.digest
					_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
					assert.ErrorContains(t, err, tc.expect)
				})
			}
		})

		t.Run("rejects an invalid access before downloading", func(t *testing.T) {
			res := githubResource("", testCommit)
			_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			assert.ErrorContains(t, err, "invalid github access")
		})

		t.Run("pins the resolved commit for a ref-only resource", func(t *testing.T) {
			res := githubResourceRef(repoURL, "main", "")
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			pinned, ok := processed.Access.(*v1.GitHub)
			require.True(t, ok, "processed access must be typed *v1.GitHub")
			assert.Equal(t, testCommit, pinned.Commit, "commit must be pinned to the resolved sha")
			require.NotNil(t, processed.Digest)
			assert.Equal(t, payloadDigest, processed.Digest.Value)

			orig := res.Access.(*v1.GitHub)
			assert.Empty(t, orig.Commit, "input resource access must not be mutated")
		})

		t.Run("resolves the ref once, not again for the download", func(t *testing.T) {
			var resolveCalls int
			repoURL := mockGitHub(t, testCommit, &resolveCalls)

			res := githubResourceRef(repoURL, "main", "")
			_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			assert.Equal(t, 1, resolveCalls, "the ref must be resolved once to pin the commit, not again for the download")
		})

		t.Run("a moved ref does not invalidate a pinned commit", func(t *testing.T) {
			// A branch that advances past the pinned commit — or is deleted
			// after a merge — must not invalidate an unchanged component
			// version. The ref resolves to a sha the mock serves no tarball
			// for, so a processor that re-resolved the pinned commit could not
			// download.
			repoURL := mockGitHub(t, strings.Repeat("1", 40), nil)
			res := githubResourceRef(repoURL, "main", testCommit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			pinned, ok := processed.Access.(*v1.GitHub)
			require.True(t, ok)
			assert.Equal(t, testCommit, pinned.Commit, "the pinned commit stays authoritative")
			assert.Equal(t, "main", pinned.Ref, "the ref is preserved as informational")
			require.NotNil(t, processed.Digest)
			assert.Equal(t, payloadDigest, processed.Digest.Value)
		})

		t.Run("pins the scheme output to a known digest", func(t *testing.T) {
			// Every other case compares against a digest computed with the same
			// library the processor uses, so a library change would move both
			// sides together. This pins the output to a literal: the well-known
			// sha256 of "abc".
			const golden = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
			res := githubResource(repoURL, testCommit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			require.NotNil(t, processed.Digest)
			assert.Equal(t, golden, processed.Digest.Value)
		})
	})

	t.Run("GetResourceDigestProcessorCredentialConsumerIdentity", func(t *testing.T) {
		identity, err := processor.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(),
			githubResource("https://github.com/open-component-model/ocm", testCommit))
		require.NoError(t, err)
		assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
	})

	t.Run("GetResourceRepositoryScheme", func(t *testing.T) {
		// The scheme's contents are the access package's contract; all this
		// processor owes is a non-nil scheme so the plugin registry can
		// dispatch to it.
		require.NotNil(t, processor.GetResourceRepositoryScheme())
	})
}
