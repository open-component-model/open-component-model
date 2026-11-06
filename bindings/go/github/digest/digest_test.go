package digest

import (
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/internal/testutil/githubtest"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// pinnedAccess decodes the access a processed resource carries. The processor
// hands it back as the runtime.Raw every other access type uses, so tests
// decode it instead of type-asserting.
func pinnedAccess(t *testing.T, res *descriptor.Resource) *v1.GitHub {
	t.Helper()
	gitHub := &v1.GitHub{}
	require.NoError(t, access.Scheme.Convert(res.Access, gitHub))
	return gitHub
}

const payload = "abc"

// payloadDigest is the generic blob digest the processor produces: sha256 over
// the exact bytes served, matching old OCM. The processor never untars or
// decompresses the download, so any bytes stand in for a real archive.
var payloadDigest = godigest.FromString(payload).Encoded()

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
		repoURL := githubtest.Start(t, githubtest.WithPayload([]byte(payload))).RepoURL

		t.Run("applies the generic blob digest of the downloaded archive", func(t *testing.T) {
			res := githubResource(repoURL, githubtest.Commit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			require.NotNil(t, processed.Digest)
			assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
			assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
			assert.Equal(t, payloadDigest, processed.Digest.Value)

			assert.Nil(t, res.Digest, "input resource must not be mutated")
		})

		t.Run("verifies a matching pre-set digest", func(t *testing.T) {
			res := githubResource(repoURL, githubtest.Commit)
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
					res := githubResource(repoURL, githubtest.Commit)
					res.Digest = &tc.digest
					_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
					assert.ErrorContains(t, err, tc.expect)
				})
			}
		})

		t.Run("rejects an invalid access before downloading", func(t *testing.T) {
			res := githubResource("", githubtest.Commit)
			_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			assert.ErrorContains(t, err, "invalid github access")
		})

		t.Run("pins the resolved commit for a ref-only resource", func(t *testing.T) {
			res := githubResourceRef(repoURL, "main", "")
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			pinned := pinnedAccess(t, processed)
			assert.Equal(t, githubtest.Commit, pinned.Commit, "commit must be pinned to the resolved sha")
			require.NotNil(t, processed.Digest)
			assert.Equal(t, payloadDigest, processed.Digest.Value)

			orig := res.Access.(*v1.GitHub)
			assert.Empty(t, orig.Commit, "input resource access must not be mutated")
		})

		t.Run("resolves the ref once, not again for the download", func(t *testing.T) {
			srv := githubtest.Start(t, githubtest.WithPayload([]byte(payload)))

			res := githubResourceRef(srv.RepoURL, "main", "")
			_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			assert.Equal(t, 1, srv.ResolveCalls(), "the ref must be resolved once to pin the commit, not again for the download")
		})

		t.Run("a moved ref does not invalidate a pinned commit", func(t *testing.T) {
			// A branch that advances past the pinned commit — or is deleted
			// after a merge — must not invalidate an unchanged component
			// version. The ref resolves to a sha the mock serves no tarball
			// for, so a processor that re-resolved the pinned commit could not
			// download.
			repoURL := githubtest.Start(t, githubtest.WithCommit(strings.Repeat("1", 40)), githubtest.WithPayload([]byte(payload))).RepoURL
			res := githubResourceRef(repoURL, "main", githubtest.Commit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			pinned := pinnedAccess(t, processed)
			assert.Equal(t, githubtest.Commit, pinned.Commit, "the pinned commit stays authoritative")
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
			res := githubResource(repoURL, githubtest.Commit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			require.NotNil(t, processed.Digest)
			assert.Equal(t, golden, processed.Digest.Value)
		})
	})

	t.Run("GetResourceDigestProcessorCredentialConsumerIdentity", func(t *testing.T) {
		identity, err := processor.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(),
			githubResource("https://github.com/open-component-model/ocm", githubtest.Commit))
		require.NoError(t, err)
		assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
	})
}
