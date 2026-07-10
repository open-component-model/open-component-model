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
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/digest"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/repository/source"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The canonical "Hello World" repository of the octocat user. Both the
// repository and this commit (its long-frozen master tip) have been stable
// for over a decade and are used widely as GitHub API test fixtures.
const (
	helloWorldRepo   = "https://github.com/octocat/Hello-World"
	helloWorldRef    = "refs/heads/master"
	helloWorldCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
)

// helloWorldAccess builds a github access; ref or commit may be empty to
// exercise the different access shapes.
func helloWorldAccess(ref, commit string) *v1.GitHub {
	return &v1.GitHub{
		Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
		RepoURL: helloWorldRepo,
		Ref:     ref,
		Commit:  commit,
	}
}

func helloWorldResource(ref, commit string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "hello-world", Version: "1.0.0"},
		},
		Access: helloWorldAccess(ref, commit),
	}
}

func helloWorldSource(ref, commit string) *descriptor.Source {
	return &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "hello-world", Version: "1.0.0"},
		},
		Access: helloWorldAccess(ref, commit),
	}
}

// assertHelloWorldArchive verifies the blob is GitHub's gzipped tar source
// archive of the Hello-World repository: matching old OCM, its entries are
// prefixed with "<repo>-<commit>/" and include the README.
func assertHelloWorldArchive(t *testing.T, downloaded blob.ReadOnlyBlob) {
	t.Helper()

	if closer, ok := downloaded.(io.Closer); ok {
		defer func() { require.NoError(t, closer.Close()) }()
	}
	reader, err := downloaded.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	gz, err := gzip.NewReader(reader)
	require.NoError(t, err)

	var readmeFound bool
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if strings.HasSuffix(header.Name, "/README") {
			readmeFound = true
		}
	}

	assert.True(t, readmeFound, "the Hello-World archive must contain its README under the commit-prefixed directory")
}

func Test_Integration_GitHub(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("resource", func(t *testing.T) {
		processor := digest.NewDigestProcessor(nil)

		t.Run("commit and ref set", func(t *testing.T) {
			t.Run("digest processing keeps the commit authoritative and the ref informational", func(t *testing.T) {
				processed, err := processor.ProcessResourceDigest(t.Context(), helloWorldResource(helloWorldRef, helloWorldCommit), nil)
				require.NoError(t, err)

				pinned, ok := processed.Access.(*v1.GitHub)
				require.True(t, ok, "processed access must be typed *v1.GitHub")
				assert.Equal(t, helloWorldCommit, pinned.Commit, "the set commit must not be re-resolved from the ref")
				assert.Equal(t, helloWorldRef, pinned.Ref, "the ref stays informational next to the pinned commit")
				require.NotNil(t, processed.Digest)
			})

			t.Run("download serves the commit source archive", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(
					t.Context(), helloWorldResource(helloWorldRef, helloWorldCommit), nil)
				require.NoError(t, err)
				assertHelloWorldArchive(t, downloaded)
			})
		})

		t.Run("commit only", func(t *testing.T) {
			res := helloWorldResource("", helloWorldCommit)
			processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err)

			t.Run("digest processing pins a SHA-256 generic blob digest without mutating the input", func(t *testing.T) {
				require.NotNil(t, processed.Digest)
				assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
				assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
				assert.Nil(t, res.Digest, "input resource must not be mutated")
			})

			t.Run("the pinned digest matches the bytes of a fresh archive download", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(t.Context(), processed, nil)
				require.NoError(t, err)
				if closer, ok := downloaded.(io.Closer); ok {
					defer func() { require.NoError(t, closer.Close()) }()
				}
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
				verified, err := processor.ProcessResourceDigest(t.Context(), processed, nil)
				require.NoError(t, err)
				assert.Equal(t, processed.Digest.Value, verified.Digest.Value)
			})
		})

		t.Run("ref only", func(t *testing.T) {
			t.Run("digest processing resolves the ref and pins the commit it points at", func(t *testing.T) {
				res := helloWorldResource(helloWorldRef, "")
				processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
				require.NoError(t, err)

				pinned, ok := processed.Access.(*v1.GitHub)
				require.True(t, ok, "processed access must be typed *v1.GitHub")
				// master has been frozen on this commit for over a decade, so
				// the resolved sha is a stable assertion target.
				assert.Equal(t, helloWorldCommit, pinned.Commit, "the ref must be resolved and pinned as a commit")
				assert.Equal(t, helloWorldRef, pinned.Ref, "the ref stays informational next to the pinned commit")
				require.NotNil(t, processed.Digest)

				orig, ok := res.Access.(*v1.GitHub)
				require.True(t, ok)
				assert.Empty(t, orig.Commit, "input resource access must not be mutated")
			})

			t.Run("download resolves the ref and serves the archive of the commit it points at", func(t *testing.T) {
				downloaded, err := resource.NewResourceRepository(nil).DownloadResource(
					t.Context(), helloWorldResource(helloWorldRef, ""), nil)
				require.NoError(t, err)
				assertHelloWorldArchive(t, downloaded)
			})
		})

		// #1069 requires the download to buffer the archive to the filesystem,
		// not hold it in memory. Point the repository at a known temp folder and
		// assert the archive lands there as a file while the blob is still open;
		// a regression to an in-memory blob leaves the folder empty.
		t.Run("archive is buffered to a file under the configured temp folder", func(t *testing.T) {
			tempFolder := t.TempDir()
			repo := resource.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder})

			downloaded, err := repo.DownloadResource(t.Context(), helloWorldResource("", helloWorldCommit), nil)
			require.NoError(t, err)
			t.Cleanup(func() {
				if closer, ok := downloaded.(io.Closer); ok {
					_ = closer.Close()
				}
			})

			entries, err := os.ReadDir(tempFolder)
			require.NoError(t, err)
			require.Len(t, entries, 1, "the archive must be buffered as a file under the configured temp folder")
			assert.True(t, strings.HasSuffix(entries[0].Name(), ".tgz"),
				"buffered archive should keep the .tgz suffix, got %q", entries[0].Name())

			assertHelloWorldArchive(t, downloaded)
		})
	})

	t.Run("source", func(t *testing.T) {
		repo := source.NewSourceRepository(nil)

		t.Run("commit and ref set", func(t *testing.T) {
			t.Run("download serves the commit source archive and ignores the ref", func(t *testing.T) {
				downloaded, err := repo.DownloadSource(t.Context(), helloWorldSource(helloWorldRef, helloWorldCommit))
				require.NoError(t, err)
				assertHelloWorldArchive(t, downloaded)
			})
		})

		t.Run("commit only", func(t *testing.T) {
			t.Run("download serves the commit source archive", func(t *testing.T) {
				downloaded, err := repo.DownloadSource(t.Context(), helloWorldSource("", helloWorldCommit))
				require.NoError(t, err)
				assertHelloWorldArchive(t, downloaded)
			})
		})

		t.Run("ref only", func(t *testing.T) {
			// Sources have no digest processor to pin a commit, so a ref-only
			// source cannot be materialized reproducibly and is rejected.
			t.Run("download is rejected without a pinned commit", func(t *testing.T) {
				_, err := repo.DownloadSource(t.Context(), helloWorldSource(helloWorldRef, ""))
				assert.ErrorContains(t, err, "requires a pinned commit")
			})
		})
	})
}
