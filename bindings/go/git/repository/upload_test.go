package repository_test

import (
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUploadGit(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	targetPath := filepath.Join(t.TempDir(), "target.git")
	target, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	uploader := repository.NewResourceRepository(nil)

	resource := func(commit plumbing.Hash) *descriptor.Resource {
		return &descriptor.Resource{Access: &accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: fixture.Path,
			Ref:        "refs/heads/main",
			Commit:     commit.String(),
		}}
	}
	first := resource(fixture.First)
	first.Digest = &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: strings.Repeat("0", 64)}
	_, err = uploader.UploadGit(t.Context(), first, repository.UploadOptions{Repository: targetPath, Ref: "refs/heads/main"}, nil, nil)
	r.ErrorContains(err, "digest mismatch")
	_, err = target.Reference("refs/heads/main", true)
	r.Error(err)
	first.Digest = nil

	for _, testCase := range []struct {
		name   string
		commit plumbing.Hash
	}{
		{name: "initial upload", commit: fixture.First},
		{name: "idempotent upload", commit: fixture.First},
		{name: "incremental upload", commit: fixture.Second},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			output, err := uploader.UploadGit(t.Context(), resource(testCase.commit), repository.UploadOptions{Repository: targetPath, Ref: "refs/heads/main"}, nil, nil)
			r.NoError(err)
			access, ok := output.Access.(*accessv1.Git)
			r.True(ok)
			r.Equal(targetPath, access.Repository)
			r.Equal("refs/heads/main", access.Ref)
			r.Equal(testCase.commit.String(), access.Commit)
			target, err = git.PlainOpen(targetPath)
			r.NoError(err)
			ref, err := target.Reference("refs/heads/main", true)
			r.NoError(err)
			r.Equal(testCase.commit, ref.Hash())
			got, err := target.CommitObject(testCase.commit)
			r.NoError(err)
			want, err := fixture.Git.CommitObject(testCase.commit)
			r.NoError(err)
			r.Equal(want.Hash, got.Hash)
			r.Equal(want.ParentHashes, got.ParentHashes)
			r.Equal(want.Author, got.Author)
		})
	}
	_, err = uploader.UploadGit(t.Context(), first, repository.UploadOptions{Repository: targetPath, Ref: "refs/heads/main"}, nil, nil)
	r.ErrorContains(err, "does not fast-forward")
}

func TestUploadGitPreservesAnnotatedTag(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	targetPath := filepath.Join(t.TempDir(), "target.git")
	target, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	sourceTag, err := fixture.Git.Reference("refs/tags/annotated", true)
	r.NoError(err)
	source := &descriptor.Resource{Access: &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: fixture.Path,
		Ref:        "refs/tags/annotated",
		Commit:     fixture.First.String(),
	}}
	uploader := repository.NewResourceRepository(nil)
	output, err := uploader.UploadGit(t.Context(), source, repository.UploadOptions{Repository: targetPath, Ref: "refs/tags/annotated"}, nil, nil)
	r.NoError(err)
	r.Equal(fixture.First.String(), output.Access.(*accessv1.Git).Commit)
	targetTag, err := target.Reference("refs/tags/annotated", true)
	r.NoError(err)
	r.Equal(sourceTag.Hash(), targetTag.Hash())
	_, err = target.TagObject(targetTag.Hash())
	r.NoError(err)
}

func TestUploadGitOptionValidation(t *testing.T) {
	source := &descriptor.Resource{Access: &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: "https://example.com/source.git",
		Commit:     strings.Repeat("a", 40),
	}}
	for _, testCase := range []struct {
		name    string
		target  repository.UploadOptions
		wantErr string
	}{
		{name: "missing target repository", target: repository.UploadOptions{Ref: "refs/heads/main"}, wantErr: "target repository is required"},
		{name: "short ref", target: repository.UploadOptions{Repository: "https://example.com/target.git", Ref: "main"}, wantErr: "target ref must be a full"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			_, err := repository.NewResourceRepository(nil).UploadGit(t.Context(), source, testCase.target, nil, nil)
			r.ErrorContains(err, testCase.wantErr)
		})
	}
}
