package transformation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemconfig "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func gitResource(t *testing.T, url, ref, commit, accessType string) *v2.Resource {
	t.Helper()
	r := require.New(t)
	raw := &runtime.Raw{}
	name, version, _ := strings.Cut(accessType, "/")
	r.NoError(access.Scheme.Convert(&accessv1.Git{
		Type: runtime.NewVersionedType(name, version), Repository: url, Ref: ref, Commit: commit,
	}, raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: "source", Version: "1.0.0"}},
		Access:      raw,
	}
}

func localRepository(t *testing.T) (path, firstCommit string) {
	t.Helper()
	r := require.New(t)
	path = t.TempDir()
	repo, err := git.PlainInit(path, false)
	r.NoError(err)
	// Ignore host signing settings when creating test fixtures.
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	worktree, err := repo.Worktree()
	r.NoError(err)
	for _, content := range []string{"original", "updated"} {
		r.NoError(os.WriteFile(filepath.Join(path, "README"), []byte(content), 0o644))
		_, err = worktree.Add("README")
		r.NoError(err)
		hash, err := worktree.Commit(content, &git.CommitOptions{Author: &object.Signature{
			Name: "Test", Email: "test@example.com", When: time.Unix(1, 0),
		}})
		r.NoError(err)
		if firstCommit == "" {
			firstCommit = hash.String()
		}
	}
	return path, firstCommit
}

func TestGetGitResourceLocalRepository(t *testing.T) {
	r := require.New(t)
	path, commit := localRepository(t)
	r.NotEmpty(commit)
	for _, tc := range []struct {
		name, ref, commit, accessType, content string
	}{
		{name: "commit", commit: commit, accessType: "Git/v1", content: "original"},
		{name: "ref", ref: "HEAD", accessType: "git", content: "updated"},
		{name: "commit overrides missing ref", ref: "refs/heads/deleted", commit: commit, accessType: "git/v1alpha1", content: "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			downloadDir, outputDir := t.TempDir(), t.TempDir()
			repo := gitrepository.NewResourceRepository(&filesystemconfig.Config{TempFolder: &downloadDir})
			resource := gitResource(t, path, tc.ref, tc.commit, tc.accessType)
			pinned, err := repo.ProcessResourceDigest(t.Context(), descriptor.ConvertFromV2Resource(resource), nil)
			r.NoError(err)
			// Keep the original access, including ref-only access, but require digest verification.
			resource.Digest = &v2.Digest{
				HashAlgorithm:          pinned.Digest.HashAlgorithm,
				NormalisationAlgorithm: pinned.Digest.NormalisationAlgorithm, Value: pinned.Digest.Value,
			}
			step := &v1alpha1.GetGitResource{
				Type: v1alpha1.GetGitResourceV1alpha1, ID: "download-git",
				Spec: &v1alpha1.GetGitResourceSpec{Resource: resource, OutputPath: outputDir},
			}
			before := step.DeepCopy()
			raw := &runtime.Raw{}
			r.NoError(v1alpha1.Scheme.Convert(step, raw))
			transformer := &GetGitResource{Scheme: v1alpha1.Scheme, ResourceRepository: repo}
			result, err := transformer.Transform(t.Context(), raw)
			r.NoError(err)
			entries, err := os.ReadDir(outputDir)
			r.NoError(err)
			r.Len(entries, 1, "only the buffered output belongs to the graph")
			var transformed v1alpha1.GetGitResource
			r.NoError(v1alpha1.Scheme.Convert(result, &transformed))
			r.Equal(before, step)
			r.Equal(step.ID, transformed.ID)
			r.Equal(resource, transformed.Output.Resource)
			outputPath, err := filesystem.FilePathFromURI(transformed.Output.File.URI)
			r.NoError(err)
			r.True(filepath.IsAbs(outputPath))
			r.Equal(outputDir, filepath.Dir(outputPath))
			r.Equal("application/x-tgz", transformed.Output.File.MediaType)
			archive, err := os.ReadFile(outputPath)
			r.NoError(err)
			r.Equal("sha256:"+resource.Digest.Value, digest.FromBytes(archive).String())
			r.Equal(digest.FromBytes(archive).String(), transformed.Output.File.Digest)
			gz, err := gzip.NewReader(bytes.NewReader(archive))
			r.NoError(err)
			defer gz.Close()
			tr := tar.NewReader(gz)
			header, err := tr.Next()
			r.NoError(err)
			r.Equal("README", header.Name)
			content, err := io.ReadAll(tr)
			r.NoError(err)
			r.Equal(tc.content, string(content))
			_, err = tr.Next()
			r.ErrorIs(err, io.EOF)
		})
	}
}

type testRepository struct {
	repository.ResourceRepository
	identity    runtime.Identity
	identityErr error
	download    func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error)
}

func (r *testRepository) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return r.identity, r.identityErr
}

func (r *testRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
	return r.download(ctx, resource, creds)
}

type resolverFunc func(context.Context, runtime.Identity) (runtime.Typed, error)

func (f resolverFunc) Resolve(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	return f(ctx, identity)
}

type testBlob struct {
	readErr error
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	return io.NopCloser(strings.NewReader("archive")), nil
}

func TestGetGitResourceCredentialsAndCleanup(t *testing.T) {
	r := require.New(t)
	failure := errors.New("test failure")
	resolved := &credsv1.GitCredentials{Token: "test-token"}
	r.NotNil(resolved)
	for _, tc := range []struct {
		name                                          string
		noProvider, noIdentity                        bool
		identityErr, resolveErr, downloadErr, readErr error
		wantDownload                                  bool
	}{
		{name: "resolved credentials", wantDownload: true},
		{name: "no provider", noProvider: true, wantDownload: true},
		{name: "no identity", noIdentity: true, wantDownload: true},
		{name: "not found", resolveErr: fmt.Errorf("wrapped: %w", credentials.ErrNotFound), wantDownload: true},
		{name: "identity failure", identityErr: failure},
		{name: "resolver failure", resolveErr: failure},
		{name: "download failure", downloadErr: failure, wantDownload: true},
		{name: "buffer failure", readErr: failure, wantDownload: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			outDir := t.TempDir()
			b := &testBlob{readErr: tc.readErr}
			downloaded, resolvedCalls := false, 0
			repo := &testRepository{identity: runtime.Identity{"type": "Git", "hostname": "example.com"}, identityErr: tc.identityErr}
			if tc.noIdentity {
				repo.identity = nil
			}
			repo.download = func(ctx context.Context, resource *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
				r.Equal(t.Context(), ctx)
				r.Equal("source", resource.Name)
				downloaded = true
				if tc.noProvider || tc.noIdentity || tc.resolveErr != nil {
					r.Nil(creds)
				} else {
					r.Same(resolved, creds)
				}
				return b, tc.downloadErr
			}
			transformer := &GetGitResource{Scheme: v1alpha1.Scheme, ResourceRepository: repo}
			if !tc.noProvider {
				transformer.CredentialProvider = resolverFunc(func(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
					r.Equal(t.Context(), ctx)
					r.Equal(repo.identity, identity)
					resolvedCalls++
					return resolved, tc.resolveErr
				})
			}
			step := &v1alpha1.GetGitResource{
				Type: v1alpha1.GetGitResourceV1alpha1,
				Spec: &v1alpha1.GetGitResourceSpec{Resource: gitResource(t, "https://example.com/repo", "HEAD", "", "Git/v1"), OutputPath: outDir},
			}
			before := step.DeepCopy()
			result, err := transformer.Transform(t.Context(), step)
			r.Equal(before, step)
			r.Equal(tc.wantDownload, downloaded)
			if tc.noProvider || tc.noIdentity || tc.identityErr != nil {
				r.Zero(resolvedCalls)
			} else {
				r.Equal(1, resolvedCalls)
			}
			if tc.identityErr != nil || errors.Is(tc.resolveErr, failure) || tc.downloadErr != nil || tc.readErr != nil {
				r.ErrorIs(err, failure)
				r.Nil(result)
				entries, err := os.ReadDir(outDir)
				r.NoError(err)
				r.Empty(entries)
			} else {
				r.NoError(err)
				r.NotNil(result)
			}
		})
	}
}

func TestGetGitResourceInvalidInput(t *testing.T) {
	r := require.New(t)
	transformer := &GetGitResource{Scheme: v1alpha1.Scheme}
	r.NotNil(transformer.Scheme)
	for _, tc := range []struct {
		name string
		step runtime.Typed
		want string
	}{
		{name: "invalid type", step: &runtime.Raw{Data: []byte(`{"type":"Unknown/v1"}`)}, want: "failed converting"},
		{name: "missing spec", step: &v1alpha1.GetGitResource{Type: v1alpha1.GetGitResourceV1alpha1}, want: "spec is required"},
		{name: "missing resource", step: &v1alpha1.GetGitResource{Type: v1alpha1.GetGitResourceV1alpha1, Spec: &v1alpha1.GetGitResourceSpec{}}, want: "resource is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			result, err := transformer.Transform(t.Context(), tc.step)
			r.ErrorContains(err, tc.want)
			r.Nil(result)
		})
	}
}

func TestGetGitResourceRemovesOutputOnBufferFailure(t *testing.T) {
	r := require.New(t)
	path, commit := localRepository(t)
	downloadDir, outDir := t.TempDir(), t.TempDir()
	repo := gitrepository.NewResourceRepository(&filesystemconfig.Config{TempFolder: &downloadDir})
	intercept := &testRepository{download: func(ctx context.Context, resource *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
		b, err := repo.DownloadResource(ctx, resource, creds)
		r.NoError(err)
		files, err := os.ReadDir(downloadDir)
		r.NoError(err)
		r.Len(files, 1, "buffer a real downloaded archive")
		// Make the already-created output path unwritable as a file, without
		// depending on permission checks that behave differently under root.
		outputs, err := os.ReadDir(outDir)
		r.NoError(err)
		r.Len(outputs, 1)
		output := filepath.Join(outDir, outputs[0].Name())
		r.NoError(os.Remove(output))
		r.NoError(os.Mkdir(output, 0o700))
		return b, nil
	}}
	transformer := &GetGitResource{Scheme: v1alpha1.Scheme, ResourceRepository: intercept}
	result, err := transformer.Transform(t.Context(), &v1alpha1.GetGitResource{
		Type: v1alpha1.GetGitResourceV1alpha1,
		Spec: &v1alpha1.GetGitResourceSpec{Resource: gitResource(t, path, "", commit, "Git/v1"), OutputPath: outDir},
	})
	r.ErrorContains(err, "failed buffering git resource archive to file")
	r.Nil(result)
	files, err := os.ReadDir(outDir)
	r.NoError(err)
	r.Empty(files)
}

func TestGetGitResourceRejectsWrongDigest(t *testing.T) {
	r := require.New(t)
	path, commit := localRepository(t)
	downloadDir, outDir := t.TempDir(), t.TempDir()
	resource := gitResource(t, path, "", commit, "Git/v1")
	resource.Digest = &v2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: strings.Repeat("0", 64)}
	transformer := &GetGitResource{Scheme: v1alpha1.Scheme, ResourceRepository: gitrepository.NewResourceRepository(&filesystemconfig.Config{TempFolder: &downloadDir})}
	result, err := transformer.Transform(t.Context(), &v1alpha1.GetGitResource{
		Type: v1alpha1.GetGitResourceV1alpha1,
		Spec: &v1alpha1.GetGitResourceSpec{Resource: resource, OutputPath: outDir},
	})
	r.ErrorContains(err, "digest mismatch")
	r.Nil(result)
	entries, err := os.ReadDir(outDir)
	r.NoError(err)
	r.Empty(entries)
}
