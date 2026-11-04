package transformation

import (
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	filesystemconfig "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestAddGitResourceLocalRepository(t *testing.T) {
	r := require.New(t)
	source, commit := localRepository(t)
	tempDir := t.TempDir()
	repo := gitrepository.NewResourceRepository(&filesystemconfig.Config{TempFolder: &tempDir})

	sourceResource, err := repo.ProcessResourceDigest(t.Context(), descriptor.ConvertFromV2Resource(gitResource(t, source, "", commit, "Git/v1")), nil)
	r.NoError(err)
	archive, err := repo.DownloadResource(t.Context(), sourceResource, nil)
	r.NoError(err)
	file, err := filesystem.BlobToSpec(archive, filepath.Join(t.TempDir(), "archive.tgz"))
	r.NoError(err)

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)
	scheme.MustRegisterScheme(access.Scheme)

	for _, tc := range []struct {
		name    string
		ref     string
		commit  string
		wantErr string
	}{
		{name: "pushes the commit to a new branch", ref: "refs/heads/mirror", commit: commit},
		{name: "pushes the commit to a new tag", ref: "refs/tags/v1.0.0", commit: commit},
		{name: "rejects an archive of another commit", ref: "refs/heads/mirror", commit: strings.Repeat("a", 40), wantErr: "commit mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			target := t.TempDir()
			_, err := git.PlainInit(target, true)
			r.NoError(err)

			resource := gitResource(t, target, tc.ref, tc.commit, "Git/v1")
			resource.Digest = &v2.Digest{
				HashAlgorithm:          sourceResource.Digest.HashAlgorithm,
				NormalisationAlgorithm: sourceResource.Digest.NormalisationAlgorithm,
				Value:                  sourceResource.Digest.Value,
			}
			transformer := &AddGitResource{Scheme: scheme, ResourceRepository: repo}
			result, err := transformer.Transform(t.Context(), &v1alpha1.AddGitResource{
				Type: v1alpha1.AddGitResourceV1alpha1,
				Spec: &v1alpha1.AddGitResourceSpec{Resource: resource, File: *file},
			})
			if tc.wantErr != "" {
				r.ErrorContains(err, "failed uploading git resource")
				r.ErrorContains(err, tc.wantErr)
				r.Nil(result)
				return
			}
			r.NoError(err)

			var transformed v1alpha1.AddGitResource
			r.NoError(scheme.Convert(result, &transformed))
			var published accessv1.Git
			r.NoError(access.Scheme.Convert(transformed.Output.Resource.Access, &published))
			r.Equal(target, published.Repository)
			r.Equal(tc.ref, published.Ref)
			r.Equal(commit, published.Commit)
			r.Equal(resource.Digest, transformed.Output.Resource.Digest)

			pushed, err := git.PlainOpen(target)
			r.NoError(err)
			ref, err := pushed.Reference(plumbing.ReferenceName(tc.ref), false)
			r.NoError(err)
			r.Equal(commit, ref.Hash().String())
		})
	}
}

func TestAddGitResourceInvalidInput(t *testing.T) {
	transformer := &AddGitResource{Scheme: v1alpha1.Scheme}
	resource := gitResource(t, "https://example.com/repo", "refs/heads/main", "", "Git/v1")
	for _, tc := range []struct {
		name string
		step runtime.Typed
		want string
	}{
		{name: "invalid type", step: &runtime.Raw{Data: []byte(`{"type":"Unknown/v1"}`)}, want: "failed converting"},
		{name: "missing spec", step: &v1alpha1.AddGitResource{Type: v1alpha1.AddGitResourceV1alpha1}, want: "spec is required"},
		{name: "missing resource", step: &v1alpha1.AddGitResource{Type: v1alpha1.AddGitResourceV1alpha1, Spec: &v1alpha1.AddGitResourceSpec{File: filesystemv1alpha1.File{URI: "file:///archive.tgz"}}}, want: "resource is required"},
		{name: "missing file", step: &v1alpha1.AddGitResource{Type: v1alpha1.AddGitResourceV1alpha1, Spec: &v1alpha1.AddGitResourceSpec{Resource: resource}}, want: "file is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			result, err := transformer.Transform(t.Context(), tc.step)
			r.ErrorContains(err, tc.want)
			r.Nil(result)
		})
	}
}
