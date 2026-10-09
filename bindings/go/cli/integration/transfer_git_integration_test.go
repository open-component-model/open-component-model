package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/cmd/configuration"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitaccess "ocm.software/open-component-model/bindings/go/git/spec/access"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// newGitFixture creates a local repository with two commits on main and returns its
// path and the first commit. A local repository exercises real Git objects without
// network access or host Git configuration deciding how fixture commits are signed.
func newGitFixture(t *testing.T) (path, first string) {
	t.Helper()
	r := require.New(t)

	path = t.TempDir()
	repo, err := git.PlainInit(path, false)
	r.NoError(err)
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	tree, err := repo.Worktree()
	r.NoError(err)
	for _, content := range []string{"first\n", "second\n"} {
		r.NoError(os.WriteFile(filepath.Join(path, "README.md"), []byte(content), 0o600))
		_, err := tree.Add("README.md")
		r.NoError(err)
		hash, err := tree.Commit(content, &git.CommitOptions{Author: &object.Signature{
			Name:  "OCM fixture",
			Email: "fixture@example.invalid",
			When:  time.Unix(1700000000, 0).UTC(),
		}})
		r.NoError(err)
		if first == "" {
			first = hash.String()
		}
	}
	return path, first
}

// assertGitArchiveAtFirstCommit checks that data is the archive of the fixture's
// first commit, not of main, which already points at the second one.
func assertGitArchiveAtFirstCommit(t *testing.T, data []byte) {
	t.Helper()
	r := require.New(t)

	gz, err := gzip.NewReader(bytes.NewReader(data))
	r.NoError(err)
	defer func() { r.NoError(gz.Close()) }()
	files := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		if header.Name == ".git" || strings.HasPrefix(header.Name, ".git/") {
			continue
		}
		content, err := io.ReadAll(tr)
		r.NoError(err)
		files[header.Name] = string(content)
	}
	r.Equal(map[string]string{"README.md": "first\n"}, files, "the archive must hold the pinned commit, not main")
}

// Test_Integration_Transfer_Git transfers a resource with a Git access from a CTF
// to an OCI registry.
//
// A repository snapshot has no OCI-artifact representation, so it can only travel
// as a local blob, and its digest is the digest of the exact archive bytes.
func Test_Integration_Transfer_Git(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	// 1. Set up the target OCI registry, an ocmconfig carrying its credentials,
	//    and the Git repository the resource is fetched from.
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")
	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{{
		Host:     targetRegistry.Host,
		Port:     targetRegistry.Port,
		User:     targetRegistry.User,
		Password: targetRegistry.Password,
	}})
	r.NoError(err)
	gitPath, commit := newGitFixture(t)

	repoProvider := provider.NewComponentVersionRepositoryProvider()
	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	r.NoError(err)
	credconf, err := runtime.LookupCredentialConfig(ocmconf)
	r.NoError(err)
	credentialResolver, err := credentials.ToGraph(ctx, credconf, credentials.Options{
		RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.RepositoryPlugin, error) {
			return nil, fmt.Errorf("no repository plugin configured for type %s", typed.GetType().String())
		}),
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed ocmruntime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no credential plugin configured for type %s", typed.GetType().String())
		}),
		CredentialRepositoryTypeScheme: ocmruntime.NewScheme(),
	})
	r.NoError(err)

	const (
		componentName    = "ocm.software/test-git-component"
		componentVersion = "v1.0.0"
		resourceName     = "repo-archive"
		resourceVersion  = "v1.0.0"
	)

	// gitConstructor renders a constructor holding one Git resource, selected by
	// pinField — "commit" or "ref".
	gitConstructor := func(pinField, pinValue string) string {
		return fmt.Sprintf(`components:
- name: %[1]s
  version: %[2]s
  provider:
    name: ocm.software
  resources:
  - name: %[3]s
    version: %[4]s
    type: directoryTree
    relation: external
    access:
      type: Git/v1
      repository: %[5]q
      %[6]s: %[7]s
`, componentName, componentVersion, resourceName, resourceVersion, gitPath, pinField, pinValue)
	}

	// addToCTF adds the constructor to a CTF in its own directory and returns the
	// stored component version's reference and the CTF path.
	addToCTF := func(t *testing.T, constructor string, extraArgs ...string) (ref, ctf string) {
		t.Helper()
		r := require.New(t)

		dir := t.TempDir()
		constructorPath := filepath.Join(dir, "constructor.yaml")
		r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

		ctf = filepath.Join(dir, "source-ctf")
		addCMD := cmd.New()
		addCMD.SetArgs(append([]string{
			"add", "component-version",
			"--repository", fmt.Sprintf("ctf::%s", ctf),
			"--constructor", constructorPath,
			"--config", cfgPath,
		}, extraArgs...))

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		r.NoError(addCMD.ExecuteContext(ctx), "creation of component version should succeed")

		return fmt.Sprintf("ctf::%s//%s:%s", ctf, componentName, componentVersion), ctf
	}

	// runTransfer transfers fromRef into a fresh repository of the target
	// registry and returns its reference and the command's error.
	runTransfer := func(t *testing.T, fromRef, repoName string, extraArgs ...string) (string, error) {
		t.Helper()

		targetRef := fmt.Sprintf("http://%s/%s", targetRegistry.RegistryAddress, repoName)
		transferCMD := cmd.New()
		transferCMD.SetArgs(append([]string{
			"transfer", "component-version",
			fromRef, targetRef,
			"--config", cfgPath,
		}, extraArgs...))

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		return targetRef, transferCMD.ExecuteContext(ctx)
	}

	// 2. Build a source CTF with one commit-pinned Git resource. Digest
	//    processing archives the commit to hash it, giving the resource the
	//    digest the transfer must preserve.
	ctfRef, sourceCTF := addToCTF(t, gitConstructor("commit", commit))

	sourceRepo, err := createRepo(ctx, repoProvider, credentialResolver, &ctfv1.Repository{FilePath: sourceCTF})
	r.NoError(err, "should be able to open the source CTF")
	sourceDesc, err := sourceRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to read the source component version")
	sourceDigest := sourceDesc.Component.Resources[0].Digest
	r.NotNil(sourceDigest, "digest processing must have hashed the archive at add time")

	// transferAndFetch transfers the source component version and returns the
	// transferred descriptor and the target reference.
	transferAndFetch := func(t *testing.T, repoName string, extraArgs ...string) (*descriptor.Descriptor, string) {
		t.Helper()
		r := require.New(t)

		targetRef, err := runTransfer(t, ctfRef, repoName, extraArgs...)
		r.NoError(err, "transfer should succeed")

		targetRepo, err := createRepo(t.Context(), repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err, "should be able to create target repository")
		desc, err := targetRepo.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err, "should be able to retrieve transferred component")
		r.Len(desc.Component.Resources, 1)
		r.Equal(resourceName, desc.Component.Resources[0].Name)
		return desc, targetRef
	}

	t.Run("copying all resources embeds the archive as a local blob", func(t *testing.T) {
		r := require.New(t)

		desc, targetRef := transferAndFetch(t, "git-transfer-default", "--copy-resources")

		res := desc.Component.Resources[0]
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(res.Access, &localBlobAccess),
			"a Git access must become a local blob once copied")
		r.Equal(transferv1alpha1.GitLocalBlobMediaType, localBlobAccess.MediaType)
		r.Empty(localBlobAccess.ReferenceName, "a by-value Git local blob must not carry a referenceName")

		// A signature over this component version covers the digest, so it must
		// survive the hop exactly as the source recorded it.
		r.NotNil(res.Digest, "the transferred resource must keep its digest")
		r.Equal("SHA-256", res.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
		r.Equal(sourceDigest.Value, res.Digest.Value,
			"the transferred digest must be the one the source CTF recorded")

		targetRepo, err := createRepo(t.Context(), repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err)
		stored := readLocalResource(t, t.Context(), targetRepo, componentName, componentVersion, res.ToIdentity())
		r.Equal(res.Digest.Value, godigest.FromBytes(stored).Encoded(),
			"the stored blob must hash to the digest it was published under")
		assertGitArchiveAtFirstCommit(t, stored)
	})

	t.Run("the default copy mode leaves the access external", func(t *testing.T) {
		r := require.New(t)

		desc, _ := transferAndFetch(t, "git-transfer-skipped")

		res := desc.Component.Resources[0]
		r.Equal("Git/v1", res.Access.GetType().String(),
			"an uncopied resource must keep its Git access")
		r.Equal(sourceDigest.Value, res.Digest.Value)
		var localBlobAccess v2.LocalBlob
		r.Error(v2.Scheme.Convert(res.Access, &localBlobAccess),
			"a Git access must not convert to a local blob")
	})

	t.Run("a git uploader pushes the commit into the target repository", func(t *testing.T) {
		r := require.New(t)

		targetGit := t.TempDir()
		_, err := git.PlainInit(targetGit, true)
		r.NoError(err)
		uploaderCfg := filepath.Join(t.TempDir(), "git-uploader.yaml")
		r.NoError(os.WriteFile(uploaderCfg, []byte(fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: git.uploader.transfer.config.ocm.software/v1alpha1
  repository: %q
  ref: refs/heads/mirror
`, targetGit)), 0o600))

		desc, targetRef := transferAndFetch(t, "git-transfer-uploader", "--config", uploaderCfg)

		res := desc.Component.Resources[0]
		r.Equal("Git/v1", res.Access.GetType().String(), "the pushed resource must be published with Git access")
		var published gitv1.Git
		r.NoError(gitaccess.Scheme.Convert(res.Access, &published))
		r.Equal(targetGit, published.Repository)
		r.Equal("refs/heads/mirror", published.Ref)
		r.Equal(commit, published.Commit, "the commit SHA must be unchanged")
		r.Equal(sourceDigest.Value, res.Digest.Value)

		pushed, err := git.PlainOpen(targetGit)
		r.NoError(err)
		ref, err := pushed.Reference("refs/heads/mirror", false)
		r.NoError(err)
		r.Equal(commit, ref.Hash().String())

		// Downloading through the published access must yield the archive the digest
		// was taken over.
		output := filepath.Join(t.TempDir(), "git-repo.tgz")
		downloadCMD := cmd.New()
		downloadCMD.SetArgs([]string{
			"download", "resource", fmt.Sprintf("%s//%s:%s", targetRef, componentName, componentVersion),
			"--identity", fmt.Sprintf("name=%s,version=%s", resourceName, resourceVersion),
			"--output", output,
			"--extraction-policy", "disable",
			"--config", cfgPath,
		})
		r.NoError(downloadCMD.ExecuteContext(t.Context()))
		archive, err := os.ReadFile(output)
		r.NoError(err)
		r.Equal(sourceDigest.Value, godigest.FromBytes(archive).Encoded())
		assertGitArchiveAtFirstCommit(t, archive)
	})

	t.Run("a resource that is not pinned to a commit is refused", func(t *testing.T) {
		r := require.New(t)

		// Skip digest processing so the stored access keeps only its ref —
		// pinning it to a commit is exactly that processor's job.
		refOnlyRef, _ := addToCTF(t, gitConstructor("ref", "refs/heads/main"), "--skip-reference-digest-processing")

		_, err := runTransfer(t, refOnlyRef, "git-transfer-refonly", "--copy-resources")
		r.Error(err, "transferring a ref-only Git resource must fail")
		r.ErrorContains(err, "no pinned commit",
			"the failure must name the missing commit, since the embedded bytes would not be reproducible")
	})
}
