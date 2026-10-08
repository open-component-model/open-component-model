package integration_test

import (
	"fmt"
	"os"
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
	upload := func(commit plumbing.Hash) *descriptor.Resource {
		source, err := resourceRepository.ProcessResourceDigest(t.Context(), &descriptor.Resource{Access: &accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: sourceURL,
			Ref:        "refs/heads/main",
			Commit:     commit.String(),
		}}, credentials)
		r.NoError(err)
		content, err := resourceRepository.DownloadResource(t.Context(), source, credentials)
		r.NoError(err)
		target := source.DeepCopy()
		target.Access = &accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: targetURL,
			Ref:        "refs/heads/release",
		}
		result, err := resourceRepository.UploadResource(t.Context(), target, content, credentials)
		r.NoError(err)
		r.Equal(source.Digest, result.Digest)
		r.Equal(&accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: targetURL,
			Ref:        "refs/heads/release",
			Commit:     commit.String(),
		}, result.Access)
		return result
	}

	upload(first)
	upload(first)
	uploaded := upload(second)
	verified, err := resourceRepository.ProcessResourceDigest(t.Context(), uploaded, credentials)
	r.NoError(err)
	r.Equal(uploaded.Digest, verified.Digest, "the target under another ref archives to the uploaded bytes")

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

func Test_Integration_GitArchiveIgnoresSourcePacking(t *testing.T) {
	r := require.New(t)
	work := t.TempDir()
	runGit(t, work, "init", "--initial-branch=main")
	// Similar revisions of one file give Git delta candidates.
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d of a file large enough to be stored as a delta", i)
	}
	for revision := range 6 {
		lines[(revision*37)%len(lines)] = fmt.Sprintf("revision %d", revision)
		r.NoError(os.WriteFile(filepath.Join(work, "data.txt"), []byte(strings.Join(lines, "\n")), 0o600))
		runGit(t, work, "add", "data.txt")
		runGit(t, work, "-c", "user.name=OCM fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false",
			"commit", "--date=2023-11-14T22:13:20Z", "-m", fmt.Sprintf("revision %d", revision))
	}
	commit := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))

	// git http-backend serves stored deltas as they are, so each layout reaches the client.
	sources := t.TempDir()
	for name, repack := range map[string][]string{
		"undeltified.git": {"repack", "-adf", "--window=0"},
		"deltified.git":   {"repack", "-adf", "--window=250", "--depth=50"},
	} {
		runGit(t, work, "clone", "--bare", "--no-local", work, filepath.Join(sources, name))
		runGit(t, filepath.Join(sources, name), repack...)
	}
	url, ca := newHTTPSServer(t, filepath.Join(sources, "undeltified.git"), "")
	trustServerCertificate(t, ca)

	tempFolder := t.TempDir()
	resourceRepository := gitrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	var digests []*descriptor.Digest
	for _, name := range []string{"undeltified.git", "deltified.git"} {
		resource, err := resourceRepository.ProcessResourceDigest(t.Context(), &descriptor.Resource{Access: &accessv1.Git{
			Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
			Repository: strings.TrimSuffix(url, "undeltified.git") + name,
			Ref:        "refs/heads/main",
			Commit:     commit,
		}}, nil)
		r.NoError(err)
		digests = append(digests, resource.Digest)

		content, err := resourceRepository.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)
		archive := filepath.Join(t.TempDir(), "resource.tar.gz")
		_, err = filesystem.BlobToSpec(content, archive)
		r.NoError(err)
		extracted := t.TempDir()
		output, err := exec.CommandContext(t.Context(), "tar", "-xzf", archive, "-C", extracted).CombinedOutput()
		r.NoError(err, string(output))
		runGit(t, extracted, "fsck", "--strict")
		r.Equal(commit, strings.TrimSpace(runGit(t, extracted, "rev-parse", "HEAD")))
	}
	r.Equal(digests[0], digests[1], "the archive must not depend on how the source packs its objects")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}
