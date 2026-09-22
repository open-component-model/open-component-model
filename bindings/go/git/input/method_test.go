package input_test

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/input"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	inputspec "ocm.software/open-component-model/bindings/go/git/spec/input"
	inputv1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestProcessResourceSnapshots(t *testing.T) {
	r := require.New(t)
	repository, first := newRepository(t)
	r.Same(inputspec.Scheme, (&input.InputMethod{}).GetInputMethodScheme())

	for _, tc := range []struct {
		name, ref, commit, want string
		raw                     bool
		maxArchiveSize          int64
	}{
		{name: "default HEAD", want: "second\n"},
		{name: "explicit ref", ref: "refs/heads/previous", want: "first\n"},
		{name: "explicit commit", commit: first, want: "first\n"},
		{name: "commit overrides ref", ref: "refs/heads/main", commit: first, want: "first\n"},
		{name: "raw unversioned alias", raw: true, want: "second\n"},
		{name: "unlimited archive", maxArchiveSize: -1, want: "second\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			spec := &inputv1.Git{
				Type: inputspec.V1VersionedType, Repository: repository, Ref: tc.ref, Commit: tc.commit,
			}
			resource := &constructorruntime.Resource{Input: spec}
			if tc.raw {
				spec.Type = runtime.NewUnversionedType("git")
				data, err := json.Marshal(spec)
				r.NoError(err)
				resource.Input = &runtime.Raw{Type: spec.Type, Data: data}
			}
			before := resource.DeepCopy()
			dir := t.TempDir()
			method := &input.InputMethod{TempFolder: dir, MaxArchiveSize: tc.maxArchiveSize}

			result, err := method.ProcessResource(t.Context(), resource, nil)
			r.NoError(err)
			r.Equal(before, resource)
			r.NotNil(result)
			r.Nil(result.ProcessedResource)
			r.NotNil(result.ProcessedBlobData)
			media, ok := result.ProcessedBlobData.(blob.MediaTypeAware)
			r.True(ok)
			mediaType, known := media.MediaType()
			r.True(known)
			r.Equal("application/x-tgz", mediaType)

			reader, err := result.ProcessedBlobData.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { r.NoError(reader.Close()) })
			gz, err := gzip.NewReader(reader)
			r.NoError(err)
			t.Cleanup(func() { r.NoError(gz.Close()) })
			archive := tar.NewReader(gz)
			header, err := archive.Next()
			r.NoError(err)
			r.Equal("README.md", header.Name)
			content, err := io.ReadAll(archive)
			r.NoError(err)
			r.Equal(tc.want, string(content))
			_, err = archive.Next()
			r.ErrorIs(err, io.EOF)

			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Len(files, 1, "only the returned archive should outlive the download")
			r.False(files[0].IsDir())
		})
	}
}

func TestProcessResourceErrors(t *testing.T) {
	r := require.New(t)
	repository, _ := newRepository(t)
	r.NotEmpty(repository)

	for _, tc := range []struct {
		name, ref, wantErr string
		maxArchiveSize     int64
		credentials        runtime.Typed
	}{
		{name: "missing ref", ref: "refs/heads/missing"},
		{name: "archive size", maxArchiveSize: 1, wantErr: "git archive exceeds the maximum size"},
		{
			name: "unsupported credentials", wantErr: "unsupported git credential type",
			credentials: &runtime.Raw{Type: runtime.NewUnversionedType("unsupported")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			resource := &constructorruntime.Resource{Input: &inputv1.Git{
				Type: inputspec.V1VersionedType, Repository: repository, Ref: tc.ref,
			}}
			before := resource.DeepCopy()
			dir := t.TempDir()
			method := &input.InputMethod{TempFolder: dir, MaxArchiveSize: tc.maxArchiveSize}

			result, err := method.ProcessResource(t.Context(), resource, tc.credentials)
			r.Error(err)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			}
			r.Nil(result)
			r.Equal(before, resource)
			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(files)
		})
	}
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	r := require.New(t)
	const repository = "ssh://git@example.com:2222/org/repo.git"
	resource := &constructorruntime.Resource{Input: &runtime.Raw{
		Type: runtime.NewUnversionedType("git"),
		Data: []byte(`{"type":"git","repository":"ssh://git@example.com:2222/org/repo.git"}`),
	}}
	before := resource.DeepCopy()
	want, err := identityv1.IdentityFromURL(repository)
	r.NoError(err)

	got, err := (&input.InputMethod{}).GetResourceCredentialConsumerIdentity(t.Context(), resource)
	r.NoError(err)
	r.Equal(want, got)
	r.Equal(before, resource)
}

func TestInvalidResource(t *testing.T) {
	r := require.New(t)
	method := &input.InputMethod{TempFolder: t.TempDir()}
	var typedNil *inputv1.Git
	for _, resource := range []*constructorruntime.Resource{
		nil,
		{},
		{Input: typedNil},
		{Input: &runtime.Raw{Type: runtime.NewUnversionedType("unsupported")}},
		{Input: &inputv1.Git{Type: inputspec.V1VersionedType}},
		{Input: &inputv1.Git{Type: inputspec.V1VersionedType, Repository: "https://example.com/repo.git", Commit: "not-a-hash"}},
	} {
		result, err := method.ProcessResource(t.Context(), resource, nil)
		r.Error(err)
		r.Nil(result)
		_, err = method.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		r.Error(err)
	}
	files, err := os.ReadDir(method.TempFolder)
	r.NoError(err)
	r.Empty(files)
}

func newRepository(t *testing.T) (path, firstCommit string) {
	t.Helper()
	r := require.New(t)
	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	r.NoError(err)
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	tree, err := repo.Worktree()
	r.NoError(err)
	for _, content := range []string{"first\n", "second\n"} {
		r.NoError(os.WriteFile(filepath.Join(work, "README.md"), []byte(content), 0o600))
		_, err := tree.Add("README.md")
		r.NoError(err)
		hash, err := tree.Commit(content, &git.CommitOptions{Author: &object.Signature{
			Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC(),
		}})
		r.NoError(err)
		if firstCommit == "" {
			firstCommit = hash.String()
			r.NoError(repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/previous", hash)))
		}
	}
	path = filepath.Join(t.TempDir(), "fixture.git")
	_, err = git.PlainClone(path, true, &git.CloneOptions{URL: work})
	r.NoError(err)
	return path, firstCommit
}
