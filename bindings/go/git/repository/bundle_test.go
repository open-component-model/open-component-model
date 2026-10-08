package repository_test

import (
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUploadResourceBundle(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	targetPath := filepath.Join(t.TempDir(), "target.git")
	target, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	tempDir := t.TempDir()
	uploader := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})

	for _, testCase := range []struct {
		name   string
		commit plumbing.Hash
	}{
		{name: "initial upload", commit: fixture.First},
		{name: "idempotent upload", commit: fixture.First},
		{name: "fast-forward update", commit: fixture.Second},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			source := &descriptor.Resource{Access: &accessv1.Git{
				Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
				Repository: fixture.Path,
				Ref:        "refs/heads/main",
				Commit:     testCase.commit.String(),
			}}
			source, err := uploader.ProcessResourceDigest(t.Context(), source, nil)
			r.NoError(err)
			bundle, err := uploader.DownloadGitBundle(t.Context(), source, nil)
			r.NoError(err)
			targetResource := source.DeepCopy()
			targetResource.Access = &accessv1.Git{
				Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
				Repository: targetPath,
				Ref:        "refs/heads/main",
				Commit:     testCase.commit.String(),
			}
			if testCase.name == "initial upload" {
				invalid := targetResource.DeepCopy()
				invalid.Digest.Value = strings.Repeat("0", 64)
				_, err := uploader.UploadResource(t.Context(), invalid, bundle, nil)
				r.ErrorContains(err, "digest mismatch")
				_, err = target.Reference("refs/heads/main", true)
				r.Error(err)
			}
			uploaded, err := uploader.UploadResource(t.Context(), targetResource, bundle, nil)
			r.NoError(err)
			r.Equal(source.Digest, uploaded.Digest)
			r.Equal(testCase.commit.String(), uploaded.Access.(*accessv1.Git).Commit)
			target, err = git.PlainOpen(targetPath)
			r.NoError(err)
			ref, err := target.Reference("refs/heads/main", true)
			r.NoError(err)
			r.Equal(testCase.commit, ref.Hash())
			commit, err := target.CommitObject(testCase.commit)
			r.NoError(err)
			original, err := fixture.Git.CommitObject(testCase.commit)
			r.NoError(err)
			r.Equal(original.Author, commit.Author)
			r.Equal(original.ParentHashes, commit.ParentHashes)
		})
	}

	_, err = uploader.UploadResource(t.Context(), &descriptor.Resource{Access: &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: targetPath,
		Ref:        "refs/heads/main",
		Commit:     fixture.Second.String(),
	}}, inmemory.New(strings.NewReader("snapshot tar bytes")), nil)
	r.ErrorContains(err, "not a Git object bundle")
}

func TestUploadResourceBundleFromAnnotatedTag(t *testing.T) {
	fixture := newRepository(t)
	tag, err := fixture.Git.Reference("refs/tags/annotated", true)
	require.NoError(t, err)
	tempDir := t.TempDir()
	uploader := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
	source := &descriptor.Resource{Access: &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: fixture.Path,
		Ref:        "refs/tags/annotated",
		Commit:     fixture.First.String(),
	}}
	bundle, err := uploader.DownloadGitBundle(t.Context(), source, nil)
	require.NoError(t, err)

	for _, testCase := range []struct {
		name      string
		targetRef string
		wantHash  plumbing.Hash
		wantTag   bool
	}{
		{name: "tag target keeps the tag object", targetRef: "refs/tags/copied", wantHash: tag.Hash(), wantTag: true},
		{name: "branch target points at the commit", targetRef: "refs/heads/release", wantHash: fixture.First},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			targetPath := filepath.Join(t.TempDir(), "target.git")
			target, err := git.PlainInit(targetPath, true)
			r.NoError(err)
			targetResource := source.DeepCopy()
			targetResource.Access = &accessv1.Git{
				Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
				Repository: targetPath,
				Ref:        testCase.targetRef,
				Commit:     fixture.First.String(),
			}
			uploaded, err := uploader.UploadResource(t.Context(), targetResource, bundle, nil)
			r.NoError(err)
			r.Equal(fixture.First.String(), uploaded.Access.(*accessv1.Git).Commit)
			ref, err := target.Reference(plumbing.ReferenceName(testCase.targetRef), true)
			r.NoError(err)
			r.Equal(testCase.wantHash, ref.Hash())
			_, err = target.TagObject(ref.Hash())
			if testCase.wantTag {
				r.NoError(err)
			} else {
				r.Error(err)
			}
		})
	}
}
