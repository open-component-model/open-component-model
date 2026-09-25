package repository_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResourceDigestPinning(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.First)))

	dir := t.TempDir()
	repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &dir})
	original := &descriptor.Resource{Access: &v1.Git{Type: runtime.NewUnversionedType("git"), Repository: fixture.Path, Ref: "refs/heads/main"}}
	before := original.DeepCopy()
	pinned, err := repo.ProcessResourceDigest(t.Context(), original, nil)
	r.NoError(err)
	r.Equal(before, original)

	var spec v1.Git
	r.NoError(access.Scheme.Convert(pinned.Access, &spec))
	r.Equal(fixture.First.String(), spec.Commit)
	r.Equal("refs/heads/main", spec.Ref)
	r.Equal("SHA-256", pinned.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", pinned.Digest.NormalisationAlgorithm)

	// Digest processing downloads into a directory of its own and removes it again.
	files, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(files)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.Second)))

	b, err := repo.DownloadResource(t.Context(), pinned, nil)
	r.NoError(err)
	r.NotNil(b)

	verified, err := repo.ProcessResourceDigest(t.Context(), pinned, nil)
	r.NoError(err)
	r.Equal(pinned, verified)

	// Only the archive handed to the caller by DownloadResource is left behind.
	files, err = os.ReadDir(dir)
	r.NoError(err)
	r.Len(files, 1)
}

func TestResourceDigestVerification(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	dir := t.TempDir()
	repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &dir})
	res := &descriptor.Resource{Access: &v1.Git{
		Type: runtime.NewVersionedType("Git", "v1"), Repository: fixture.Path, Commit: fixture.First.String(),
	}}

	b, err := repo.DownloadResource(t.Context(), res, nil)
	r.NoError(err)

	reader, err := b.ReadCloser()
	r.NoError(err)
	compressed, err := io.ReadAll(reader)
	r.NoError(err)
	r.NoError(reader.Close())
	checksum := digest.FromBytes(compressed)

	generated, err := repo.ProcessResourceDigest(t.Context(), res, nil)
	r.NoError(err)
	r.Equal(&descriptor.Digest{
		HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: checksum.Encoded(),
	}, generated.Digest)

	for _, tc := range []struct {
		name, value, hashAlgorithm, normalisation, wantErr string
	}{
		{name: "compressed digest", value: checksum.Encoded()},
		{name: "wrong digest", value: strings.Repeat("0", 64), wantErr: "digest mismatch"},
		{name: "unsupported hash", value: checksum.Encoded(), hashAlgorithm: "SHA-512", wantErr: "unsupported git hash algorithm"},
		{name: "unsupported normalisation", value: checksum.Encoded(), normalisation: "other", wantErr: "unsupported git normalisation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			preset := res.DeepCopy()
			preset.Digest = generated.Digest.DeepCopy()
			preset.Digest.Value = tc.value
			if tc.hashAlgorithm != "" {
				preset.Digest.HashAlgorithm = tc.hashAlgorithm
			}
			if tc.normalisation != "" {
				preset.Digest.NormalisationAlgorithm = tc.normalisation
			}
			before := preset.DeepCopy()

			_, downloadErr := repo.DownloadResource(t.Context(), preset, nil)
			processed, processErr := repo.ProcessResourceDigest(t.Context(), preset, nil)
			if tc.wantErr == "" {
				r.NoError(downloadErr)
				r.NoError(processErr)
				r.Equal(preset.Digest, processed.Digest)
			} else {
				r.ErrorContains(downloadErr, tc.wantErr)
				r.ErrorContains(processErr, tc.wantErr)
				r.Nil(processed)
			}
			r.Equal(before, preset)
		})
	}
}

func TestResourceDigestKeepsPinnedCommit(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	tag, err := fixture.Git.Reference("refs/tags/annotated", false)
	r.NoError(err)

	// The annotated tag object peels to fixture.First, but the pinned value is kept.
	res := &descriptor.Resource{Access: &v1.Git{Type: runtime.NewUnversionedType("git"), Repository: fixture.Path, Commit: tag.Hash().String()}}
	tempDir := t.TempDir()
	pinned, err := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir}).ProcessResourceDigest(t.Context(), res, nil)
	r.NoError(err)

	var spec v1.Git
	r.NoError(access.Scheme.Convert(pinned.Access, &spec))
	r.Equal(tag.Hash().String(), spec.Commit)
}

func TestInvalidResource(t *testing.T) {
	r := require.New(t)

	repo := repository.NewResourceRepository(nil)
	var typedNil *v1.Git
	for _, res := range []*descriptor.Resource{nil, {}, {Access: typedNil}, {Access: &runtime.Raw{Type: runtime.NewUnversionedType("wrong")}}} {
		_, err := repo.DownloadResource(t.Context(), res, nil)
		r.Error(err)

		_, err = repo.GetResourceCredentialConsumerIdentity(t.Context(), res)
		r.Error(err)

		_, err = repo.ProcessResourceDigest(t.Context(), res, nil)
		r.Error(err)
	}

	_, err := repo.UploadResource(t.Context(), nil, nil, nil)
	r.ErrorContains(err, "do not support upload")
	r.Same(access.Scheme, repo.GetResourceRepositoryScheme())
}

type repositoryFixture struct {
	Path          string
	Git           *git.Repository
	First, Second plumbing.Hash
}

// newRepository creates a bare repository with two commits on main and an
// annotated tag on the first. The wrapper needs a moving branch to pin and a
// tag object to peel; ref resolution and archive contents are covered in
// internal/download.
func newRepository(t *testing.T) repositoryFixture {
	t.Helper()

	r := require.New(t)

	signature := &object.Signature{
		Name:  "OCM fixture",
		Email: "fixture@example.invalid",
		When:  time.Unix(1700000000, 0).UTC(),
	}

	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	r.NoError(err)
	// go-git reads the global Git config; a host commit.gpgSign must not sign fixtures.
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

	tree, err := repo.Worktree()
	r.NoError(err)

	commit := func(content string) plumbing.Hash {
		r.NoError(os.WriteFile(filepath.Join(work, "README.md"), []byte(content), 0o600))

		_, err := tree.Add("README.md")
		r.NoError(err)

		hash, err := tree.Commit(content, &git.CommitOptions{Author: signature})
		r.NoError(err)

		return hash
	}

	first := commit("first\n")
	second := commit("second\n")

	path := filepath.Join(t.TempDir(), "fixture.git")
	bare, err := git.PlainClone(path, &git.CloneOptions{URL: work, Bare: true})
	r.NoError(err)

	_, err = bare.CreateTag("annotated", first, &git.CreateTagOptions{Tagger: signature, Message: "release\n"})
	r.NoError(err)

	return repositoryFixture{Path: path, Git: bare, First: first, Second: second}
}
