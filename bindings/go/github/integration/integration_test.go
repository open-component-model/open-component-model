package integration

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/digest"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/repository/source"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// This repository, pinned at a published release tag. The ref-only cases below
// assert that resolving the ref yields exactly ocmCommit, so the ref must never
// move: a release tag of a published version is the strongest such guarantee a
// live repository offers (branches advance, release tags do not).
const (
	ocmRepo   = "https://github.com/open-component-model/open-component-model"
	ocmRef    = "refs/tags/v0.8.0"
	ocmCommit = "b4bb4e880aa5c159366db7cc2ae800e1ee14dbda"
)

// testCredentials authenticates the run when GITHUB_TOKEN is set and stays
// anonymous otherwise. Unauthenticated GitHub allows only 60 requests per hour
// per IP, and this test makes ~10 — on shared CI egress the token is what
// keeps the run from flaking on rate limits.
func testCredentials() runtime.Typed {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil
	}
	return &credsv1.GitHubCredentials{
		Type:  runtime.NewVersionedType(credsv1.GitHubCredentialsType, credsv1.Version),
		Token: token,
	}
}

// ocmAccess builds a github access; ref or commit may be empty to exercise the
// different access shapes.
func ocmAccess(ref, commit string) *v1.GitHub {
	return &v1.GitHub{
		Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
		RepoURL: ocmRepo,
		Ref:     ref,
		Commit:  commit,
	}
}

func ocmResource(ref, commit string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "open-component-model", Version: "1.0.0"},
		},
		Access: ocmAccess(ref, commit),
	}
}

func ocmSource(ref, commit string) *descriptor.Source {
	return &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "open-component-model", Version: "1.0.0"},
		},
		Access: ocmAccess(ref, commit),
	}
}

// Beside the pax global header, the REST tarball endpoint roots every archive
// entry at a single directory named "<owner>-<repo>-<abbreviated-commit>/".
const ocmArchiveRoot = "open-component-model-open-component-model-"

// assertOCMArchive verifies the blob is GitHub's gzipped tar source archive of
// this repository at ocmCommit.
//
// It asserts the commit, not just the presence of a README.md: every revision
// of this repository has a README.md, so a download that ignored the pinned
// commit would satisfy a README-only check. Two parts of the payload name the
// commit — the pax global header records it in full, the root directory
// abbreviated.
func assertOCMArchive(t *testing.T, downloaded blob.ReadOnlyBlob) {
	t.Helper()

	reader, err := downloaded.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	gz, err := gzip.NewReader(reader)
	require.NoError(t, err)

	var commitFound, readmeFound bool
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// git names the commit an archive was cut from in the "comment" record
		// of the pax global header, as the full sha.
		if header.Typeflag == tar.TypeXGlobalHeader {
			assert.Equal(t, ocmCommit, header.PAXRecords["comment"],
				"the archive must be cut from the commit the download was asked for")
			commitFound = true
			continue
		}

		root, _, ok := strings.Cut(header.Name, "/")
		require.True(t, ok, "archive entry %q must live under the root directory", header.Name)
		abbrev, ok := strings.CutPrefix(root, ocmArchiveRoot)
		require.True(t, ok, "archive entry %q must be rooted at %q<commit>", header.Name, ocmArchiveRoot)
		// git abbreviates the sha only as far as it must to stay unambiguous,
		// so match a prefix of the commit instead of a fixed width.
		assert.True(t, abbrev != "" && strings.HasPrefix(ocmCommit, abbrev),
			"archive is rooted at commit %q, but the download was asked for %q", abbrev, ocmCommit)

		if root+"/README.md" == header.Name {
			readmeFound = true
		}
	}

	assert.True(t, commitFound, "the archive must carry the pax global header naming its commit")
	assert.True(t, readmeFound, "the archive must contain the repository's README.md under the commit-prefixed root")
}

func Test_Integration_GitHub(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("resource", func(t *testing.T) {
		processor := digest.NewDigestProcessor(nil)

		t.Run("commit and ref set", func(t *testing.T) {
			t.Run("digest processing keeps the commit authoritative and the ref informational", func(t *testing.T) {
				processed, err := processor.ProcessResourceDigest(t.Context(), ocmResource(ocmRef, ocmCommit), testCredentials())
				require.NoError(t, err)

				pinned, ok := processed.Access.(*v1.GitHub)
				require.True(t, ok, "processed access must be typed *v1.GitHub")
				assert.Equal(t, ocmCommit, pinned.Commit, "the set commit must not be re-resolved from the ref")
				assert.Equal(t, ocmRef, pinned.Ref, "the ref stays informational next to the pinned commit")
				require.NotNil(t, processed.Digest)
			})

			t.Run("download serves the commit source archive", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(
					t.Context(), ocmResource(ocmRef, ocmCommit), testCredentials())
				require.NoError(t, err)
				assertOCMArchive(t, downloaded)
			})
		})

		t.Run("commit only", func(t *testing.T) {
			res := ocmResource("", ocmCommit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, testCredentials())
			require.NoError(t, err)

			t.Run("digest processing pins a SHA-256 generic blob digest without mutating the input", func(t *testing.T) {
				require.NotNil(t, processed.Digest)
				assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
				assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
				assert.Nil(t, res.Digest, "input resource must not be mutated")
			})

			t.Run("the pinned digest matches the bytes of a fresh archive download", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(t.Context(), processed, testCredentials())
				require.NoError(t, err)
				reader, err := downloaded.ReadCloser()
				require.NoError(t, err)
				defer func() { require.NoError(t, reader.Close()) }()

				archiveDigest, err := godigest.FromReader(reader)
				require.NoError(t, err)
				assert.Equal(t, archiveDigest.Encoded(), processed.Digest.Value,
					"the pinned digest must be the generic blob digest of the exact archive GitHub serves")
			})

			// Re-processing a resource that already carries a digest exercises
			// the verification branch against the real GitHub archive. It only
			// passes if GitHub serves byte-identical archive bytes for the same
			// commit — the reproducibility assumption the digest design relies on.
			t.Run("re-processing a digested resource verifies it against a reproducible archive", func(t *testing.T) {
				verified, err := processor.ProcessResourceDigest(t.Context(), processed, testCredentials())
				require.NoError(t, err)
				assert.Equal(t, processed.Digest.Value, verified.Digest.Value)
			})
		})

		t.Run("ref only", func(t *testing.T) {
			t.Run("digest processing resolves the ref and pins the commit it points at", func(t *testing.T) {
				res := ocmResource(ocmRef, "")
				processed, err := processor.ProcessResourceDigest(t.Context(), res, testCredentials())
				require.NoError(t, err)

				pinned, ok := processed.Access.(*v1.GitHub)
				require.True(t, ok, "processed access must be typed *v1.GitHub")
				// The ref is a published release tag, so the sha it resolves to
				// is a stable assertion target.
				assert.Equal(t, ocmCommit, pinned.Commit, "the ref must be resolved and pinned as a commit")
				assert.Equal(t, ocmRef, pinned.Ref, "the ref stays informational next to the pinned commit")
				require.NotNil(t, processed.Digest)

				orig, ok := res.Access.(*v1.GitHub)
				require.True(t, ok)
				assert.Empty(t, orig.Commit, "input resource access must not be mutated")
			})

			t.Run("download resolves the ref and serves the archive of the commit it points at", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(
					t.Context(), ocmResource(ocmRef, ""), testCredentials())
				require.NoError(t, err)
				assertOCMArchive(t, downloaded)
			})
		})
	})

	t.Run("source", func(t *testing.T) {
		repo := source.NewSourceRepository(nil)

		t.Run("commit and ref set", func(t *testing.T) {
			t.Run("download serves the commit source archive and ignores the ref", func(t *testing.T) {
				downloaded, err := repo.DownloadSource(t.Context(), ocmSource(ocmRef, ocmCommit))
				require.NoError(t, err)
				assertOCMArchive(t, downloaded)
			})
		})

		t.Run("commit only", func(t *testing.T) {
			t.Run("download serves the commit source archive", func(t *testing.T) {
				downloaded, err := repo.DownloadSource(t.Context(), ocmSource("", ocmCommit))
				require.NoError(t, err)
				assertOCMArchive(t, downloaded)
			})
		})

		t.Run("ref only", func(t *testing.T) {
			// Sources have no digest processor to pin a commit, so a ref-only
			// source cannot be materialized reproducibly and is rejected.
			t.Run("download is rejected without a pinned commit", func(t *testing.T) {
				_, err := repo.DownloadSource(t.Context(), ocmSource(ocmRef, ""))
				assert.ErrorContains(t, err, "requires a pinned commit")
			})
		})
	})
}
