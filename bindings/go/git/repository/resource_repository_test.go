package repository_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

	wrong := pinned.DeepCopy()
	wrong.Digest.Value = strings.Repeat("0", 64)
	_, err = repo.DownloadResource(t.Context(), wrong, nil)
	r.ErrorContains(err, "digest mismatch")

	wrong = pinned.DeepCopy()
	wrong.Digest.HashAlgorithm = "SHA-512"
	_, err = repo.DownloadResource(t.Context(), wrong, nil)
	r.ErrorContains(err, "unsupported git hash algorithm")

	wrong = pinned.DeepCopy()
	wrong.Digest.NormalisationAlgorithm = "other"
	_, err = repo.DownloadResource(t.Context(), wrong, nil)
	r.ErrorContains(err, "unsupported git normalisation")
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
	bare, err := git.PlainClone(path, true, &git.CloneOptions{URL: work})
	r.NoError(err)

	_, err = bare.CreateTag("annotated", first, &git.CreateTagOptions{Tagger: signature, Message: "release\n"})
	r.NoError(err)

	return repositoryFixture{Path: path, Git: bare, First: first, Second: second}
}
