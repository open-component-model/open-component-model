package repository_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"

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
	repo := repository.NewResourceRepository(repository.WithTempDir(dir))
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

	files, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(files)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.Second)))

	b, err := repo.DownloadResource(t.Context(), pinned, nil)
	r.NoError(err)
	r.NoError(b.(io.Closer).Close())

	verified, err := repo.ProcessResourceDigest(t.Context(), pinned, nil)
	r.NoError(err)
	r.Equal(pinned, verified)

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

	files, err = os.ReadDir(dir)
	r.NoError(err)
	r.Empty(files)
}

func TestInvalidResource(t *testing.T) {
	r := require.New(t)

	repo := repository.NewResourceRepository()
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
