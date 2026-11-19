package input_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/input"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	inputspec "ocm.software/open-component-model/bindings/go/git/spec/input"
	inputv1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	testgit "ocm.software/open-component-model/bindings/go/testutils/git"
)

func TestProcessResourceSnapshots(t *testing.T) {
	r := require.New(t)
	repository, first := newRepository(t)
	r.Same(inputspec.Scheme, (&input.InputMethod{}).GetInputMethodScheme())

	for _, tc := range []struct {
		name, ref, commit, want string
	}{
		{name: "default HEAD", want: "second\n"},
		{name: "explicit ref", ref: "refs/heads/previous", want: "first\n"},
		{name: "explicit tag", ref: "refs/tags/v1", want: "first\n"},
		{name: "explicit commit", commit: first, want: "first\n"},
		{name: "commit overrides ref", ref: "refs/heads/main", commit: first, want: "first\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			spec := &inputv1.Git{
				Type: inputspec.V1VersionedType, Repository: repository, Ref: tc.ref, Commit: tc.commit,
			}
			resource := &constructorruntime.Resource{Input: spec}
			before := resource.DeepCopy()
			dir := t.TempDir()
			method := &input.InputMethod{TempFolder: dir}

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
			gz, err := gzip.NewReader(reader)
			r.NoError(err)
			t.Cleanup(func() { r.NoError(gz.Close()) })
			archived := map[string]string{}
			archive := tar.NewReader(gz)
			for {
				header, err := archive.Next()
				if err == io.EOF {
					break
				}
				r.NoError(err)
				if header.Name == ".git" || strings.HasPrefix(header.Name, ".git/") {
					continue
				}
				content, err := io.ReadAll(archive)
				r.NoError(err)
				archived[header.Name] = string(content)
			}
			r.Equal(map[string]string{"README.md": tc.want}, archived)

			// The clone serving the read is removed once the reader is closed.
			_, err = io.Copy(io.Discard, reader)
			r.NoError(err)
			r.NoError(reader.Close())
			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(files, "closing the reader removes the clone; the archive is never stored")
		})
	}
}

// processAndRead processes resource and reads the archive to its end, which is when a
// download fails.
func processAndRead(ctx context.Context, method *input.InputMethod, resource *constructorruntime.Resource, creds runtime.Typed) error {
	result, err := method.ProcessResource(ctx, resource, creds)
	if err != nil {
		return err
	}
	rc, err := result.ProcessedBlobData.ReadCloser()
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, rc)
	return errors.Join(err, rc.Close())
}

func TestProcessResourceErrors(t *testing.T) {
	r := require.New(t)
	repository, _ := newRepository(t)
	r.NotEmpty(repository)

	for _, tc := range []struct {
		name, wantErr  string
		maxArchiveSize *int64
		credentials    runtime.Typed
	}{
		{name: "archive size", maxArchiveSize: new(int64(1)), wantErr: "git archive exceeds the maximum size"},
		{
			name: "unsupported credentials", wantErr: "unsupported git credential type",
			credentials: &runtime.Raw{Type: runtime.NewUnversionedType("unsupported")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			resource := &constructorruntime.Resource{Input: &inputv1.Git{
				Type: inputspec.V1VersionedType, Repository: repository,
			}}
			before := resource.DeepCopy()
			dir := t.TempDir()
			method := &input.InputMethod{TempFolder: dir, MaxArchiveSize: tc.maxArchiveSize}

			err := processAndRead(t.Context(), method, resource, tc.credentials)
			r.ErrorContains(err, tc.wantErr)
			r.Equal(before, resource)
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

func TestGetResourceCredentialConsumerIdentityIgnoresFragment(t *testing.T) {
	r := require.New(t)
	resource := &constructorruntime.Resource{Input: &inputv1.Git{
		Type:       inputspec.V1VersionedType,
		Repository: "git@example.com:org/repo.git#branch=main",
	}}
	want, err := identityv1.IdentityFromURL("git@example.com:org/repo.git")
	r.NoError(err)

	got, err := (&input.InputMethod{}).GetResourceCredentialConsumerIdentity(t.Context(), resource)
	r.NoError(err)
	r.Equal(want, got)
}

func TestInvalidResource(t *testing.T) {
	r := require.New(t)
	method := &input.InputMethod{TempFolder: t.TempDir()}
	for _, resource := range []*constructorruntime.Resource{
		nil,
		{},
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
}

func newRepository(t *testing.T) (path, firstCommit string) {
	t.Helper()
	path, first := testgit.Repository(t, testgit.Options{
		DefaultBranch: "main",
		Bare:          true,
		Branches:      []string{"previous"},
		Tags:          []string{"v1"},
	})
	return path, first.String()
}
