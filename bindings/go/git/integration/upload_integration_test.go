package integration_test

import (
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_GitUploadOverHTTPS(t *testing.T) {
	r := require.New(t)

	sourcePath, first := newRepository(t)
	sourceRepo, err := git.PlainOpen(sourcePath)
	r.NoError(err)
	main, err := sourceRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
	r.NoError(err)
	second := main.Hash()

	targetPath := filepath.Join(filepath.Dir(sourcePath), "target.git")
	targetRepo, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	configureGitRepository(t, sourcePath, "uploadpack.allowFilter", "true")
	configureGitRepository(t, targetPath, "http.receivepack", "true")

	const token = "fixture-upload-token"
	sourceURL, ca := newHTTPSServer(t, sourcePath, "Bearer "+token)
	trustServerCertificate(t, ca)
	targetURL := strings.TrimSuffix(sourceURL, filepath.Base(sourcePath)) + filepath.Base(targetPath)
	credentials := &credsv1.GitCredentials{
		Type:  runtime.NewVersionedType(credsv1.GitCredentialsType, credsv1.Version),
		Token: token,
	}

	tempFolder := t.TempDir()
	resourceRepository := gitrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	resourceFor := func(commit plumbing.Hash) *descriptor.Resource {
		resource := &descriptor.Resource{Access: &accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: sourceURL,
			Ref:        "refs/heads/main",
			Commit:     commit.String(),
			Depth:      1,
			Filter:     "blob:none",
		}}
		pinned, err := resourceRepository.ProcessResourceDigest(t.Context(), resource, credentials)
		r.NoError(err)
		return pinned
	}
	upload := func(resource *descriptor.Resource) *descriptor.Resource {
		result, err := resourceRepository.UploadGit(t.Context(), resource, gitrepository.UploadOptions{
			Repository: targetURL,
			Ref:        "refs/heads/release",
		}, credentials, credentials)
		r.NoError(err)
		r.Equal(resource.Digest, result.Digest)
		r.Equal(targetURL, result.Access.(*accessv1.Git).Repository)
		r.Equal("refs/heads/release", result.Access.(*accessv1.Git).Ref)
		return result
	}

	firstResource := resourceFor(first)
	upload(firstResource)
	upload(firstResource)
	secondResource := resourceFor(second)
	upload(secondResource)
	bundle, err := resourceRepository.DownloadGitBundle(t.Context(), secondResource, credentials)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(bundle.(io.Closer).Close()) })
	bundlePath := filepath.Join(t.TempDir(), "source.bundle")
	_, err = filesystem.BlobToSpec(bundle, bundlePath)
	r.NoError(err)
	verified, err := exec.CommandContext(t.Context(), "git", "-C", targetPath, "bundle", "verify", bundlePath).CombinedOutput()
	r.NoError(err, string(verified))
	bundleTarget := secondResource.DeepCopy()
	bundleTarget.Access = &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: targetURL,
		Ref:        "refs/heads/bundle",
		Commit:     second.String(),
	}
	bundleResult, err := resourceRepository.UploadResource(t.Context(), bundleTarget, bundle, credentials)
	r.NoError(err)
	r.Equal(second.String(), bundleResult.Access.(*accessv1.Git).Commit)

	ref, err := targetRepo.Reference("refs/heads/release", true)
	r.NoError(err)
	r.Equal(second, ref.Hash())
	for _, hash := range []plumbing.Hash{first, second} {
		commit, err := targetRepo.CommitObject(hash)
		r.NoError(err)
		sourceCommit, err := sourceRepo.CommitObject(hash)
		r.NoError(err)
		r.Equal(sourceCommit.Hash, commit.Hash)
		r.Equal(sourceCommit.ParentHashes, commit.ParentHashes)
	}
}

func configureGitRepository(t *testing.T, repositoryPath, key, value string) {
	t.Helper()
	r := require.New(t)
	command := exec.CommandContext(t.Context(), "git", "-C", repositoryPath, "config", key, value)
	output, err := command.CombinedOutput()
	r.NoError(err, string(output))
}
