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
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/cmd/configuration"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	gitaccess "ocm.software/open-component-model/bindings/go/git/spec/access"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	testgit "ocm.software/open-component-model/bindings/go/testutils/git"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func Test_Integration_AddGitFragmentWithoutDigestProcessing(t *testing.T) {
	for _, testCase := range []struct {
		name, fragment, ref, commit string
	}{
		{name: "branch", fragment: "branch=refs/heads/main", ref: "refs/heads/main"},
		{name: "tag", fragment: "tag=refs/tags/v1", ref: "refs/tags/v1"},
		{name: "commit", fragment: "commit=" + strings.Repeat("a", 40), commit: strings.Repeat("a", 40)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			constructorPath := filepath.Join(dir, "constructor.yaml")
			configPath := filepath.Join(dir, "ocmconfig.yaml")
			r.NoError(os.WriteFile(configPath, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))
			constructor := fmt.Sprintf(`components:
- name: example.com/git-fragment
  version: 1.0.0
  provider:
    name: example.com
  resources:
  - name: repo
    version: 1.0.0
    type: directoryTree
    access:
      type: Git/v1
      repository: https://example.invalid/repo.git#%[1]s
  sources:
  - name: source
    version: 1.0.0
    type: directoryTree
    access:
      type: Git/v1
      repository: https://example.invalid/repo.git#%[1]s
`, testCase.fragment)
			r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))
			ctf := filepath.Join(dir, "ctf")
			command := cmd.New()
			command.SetArgs([]string{
				"add", "component-version", "--repository", "ctf::" + ctf,
				"--constructor", constructorPath, "--config", configPath, "--skip-reference-digest-processing",
			})
			r.NoError(command.ExecuteContext(t.Context()))
			repo, err := provider.NewComponentVersionRepositoryProvider().GetComponentVersionRepository(t.Context(), &ctfv1.Repository{FilePath: ctf}, nil)
			r.NoError(err)
			desc, err := repo.GetComponentVersion(t.Context(), "example.com/git-fragment", "1.0.0")
			r.NoError(err)
			r.Len(desc.Component.Resources, 1)
			resource := desc.Component.Resources[0]
			r.Nil(resource.Digest)
			var access gitv1.Git
			r.NoError(gitaccess.Scheme.Convert(resource.Access, &access))
			r.Equal("https://example.invalid/repo.git", access.Repository)
			r.Equal(testCase.ref, access.Ref)
			r.Equal(testCase.commit, access.Commit)
			r.Len(desc.Component.Sources, 1)
			var sourceAccess gitv1.Git
			r.NoError(gitaccess.Scheme.Convert(desc.Component.Sources[0].Access, &sourceAccess))
			r.Equal(access, sourceAccess)
		})
	}
}

// newGitFixture creates a local repository with two commits on main and returns its
// path and the first commit. A local repository exercises real Git objects without
// network access or host Git configuration deciding how fixture commits are signed.
func newGitFixture(t *testing.T) (path, first string) {
	t.Helper()
	repoPath, firstCommit := testgit.Repository(t, testgit.Options{DefaultBranch: "main"})
	return repoPath, firstCommit.String()
}

// assertGitArchiveAtFirstCommit checks that data is the archive of the fixture's
// first commit, not of main, which already points at the second one.
func assertGitArchiveAtFirstCommit(t *testing.T, data []byte) {
	t.Helper()
	assertGitArchiveREADME(t, data, "first\n")
}

// assertGitArchiveREADME checks that data is a Git archive whose README.md holds want.
func assertGitArchiveREADME(t *testing.T, data []byte, want string) {
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
	r.Equal(map[string]string{"README.md": want}, files, "unexpected Git archive content")
}

// newBareGitRepository creates an empty bare repository and returns its path and URL.
func newBareGitRepository(t *testing.T) (path, url string) {
	t.Helper()
	path = t.TempDir()
	_, err := git.PlainInit(path, true)
	require.NoError(t, err)
	return path, testgit.Serve(t, path)
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

	// Create the fixture locally to get the commit hash, then expose it over HTTP
	// so all accesses use a remote URL (local-path git accesses are rejected by Validate).
	gitPath, commit := newGitFixture(t)
	gitServerURL := testgit.Serve(t, gitPath)

	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithHTTPConfig(loadHTTPConfig(t, cfgPath)))
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

	// seedCTF writes a component version with a remote Git access into a CTF,
	// using the HTTP git server for the source repository. pinDigest controls
	// whether ProcessResourceDigest is called to pin the commit and record the
	// archive digest (mirrors --skip-reference-digest-processing).
	seedCTF := func(t *testing.T, ref, commit string, pinDigest bool) (ctfRef, ctfPath string) {
		t.Helper()
		r := require.New(t)

		gitRepo := gitrepository.NewResourceRepository(nil)

		access := &gitv1.Git{
			Type:       ocmruntime.NewVersionedType(gitv1.Type, gitv1.Version),
			Repository: gitServerURL,
			Ref:        ref,
			Commit:     commit,
		}
		raw := &ocmruntime.Raw{}
		r.NoError(gitaccess.Scheme.Convert(access, raw))

		res := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: resourceVersion},
			},
			Type:     "directoryTree",
			Relation: descriptor.ExternalRelation,
			Access:   raw,
		}

		if pinDigest {
			pinned, err := gitRepo.ProcessResourceDigest(t.Context(), &res, nil)
			r.NoError(err, "digest processing must succeed for remote git repository")
			res = *pinned
		}

		ctfPath = filepath.Join(t.TempDir(), "source-ctf")
		repo, err := createRepo(t.Context(), repoProvider, credentialResolver, &ctfv1.Repository{
			FilePath:   ctfPath,
			AccessMode: ctfv1.AccessModeCreate + "|" + ctfv1.AccessModeReadWrite,
		})
		r.NoError(err)
		r.NoError(repo.AddComponentVersion(t.Context(), &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "ocm.software"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion}},
				Resources:     []descriptor.Resource{res},
			},
		}))

		return fmt.Sprintf("ctf::%s//%s:%s", ctfPath, componentName, componentVersion), ctfPath
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

	// 2. Build a source CTF with one commit-pinned Git resource using the remote HTTP
	//    server. The commit from newGitFixture is the first commit; the HTTP server
	//    serves the full history so pinning to that commit resolves correctly.
	ctfRef, sourceCTF := seedCTF(t, "refs/heads/main", commit, true)

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

		// Use a remote HTTP git server as the upload target so the git uploader
		// can push without hitting the local-path rejection in Validate.
		targetGitPath, targetGitURL := newBareGitRepository(t)
		uploaderCfg := filepath.Join(t.TempDir(), "git-uploader.yaml")
		r.NoError(os.WriteFile(uploaderCfg, []byte(fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: git.uploader.transfer.config.ocm.software/v1alpha1
  repository: %q
  ref: refs/heads/mirror
`, targetGitURL)), 0o600))

		desc, targetRef := transferAndFetch(t, "git-transfer-uploader", "--config", uploaderCfg)

		res := desc.Component.Resources[0]
		r.Equal("Git/v1", res.Access.GetType().String(), "the pushed resource must be published with Git access")
		var published gitv1.Git
		r.NoError(gitaccess.Scheme.Convert(res.Access, &published))
		r.Equal(targetGitURL, published.Repository)
		r.Equal("refs/heads/mirror", published.Ref)
		r.Equal(commit, published.Commit, "the commit SHA must be unchanged")
		r.Equal(sourceDigest.Value, res.Digest.Value)

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

		pushed, err := git.PlainOpen(targetGitPath)
		r.NoError(err)
		ref, err := pushed.Reference("refs/heads/mirror", false)
		r.NoError(err)
		r.Equal(commit, ref.Hash().String(), "the commit must land on refs/heads/mirror")
	})

	t.Run("a resource that is not pinned to a commit is transferred at the ref's commit", func(t *testing.T) {
		r := require.New(t)

		// Skip digest processing so the stored access keeps only its ref — pinning it to a
		// commit is that processor's job. Transfer no longer refuses a ref-only resource; it
		// embeds the archive at the commit the ref resolves to now (main's second commit),
		// matching how an unpinned OCI resource transfers by tag without a digest.
		refOnlyRef, _ := seedCTF(t, "refs/heads/main", "", false)

		targetRef, err := runTransfer(t, refOnlyRef, "git-transfer-refonly", "--copy-resources")
		r.NoError(err, "transferring a ref-only Git resource must succeed")

		targetRepo, err := createRepo(t.Context(), repoProvider, credentialResolver, &ociv1.Repository{BaseUrl: targetRef})
		r.NoError(err)
		desc, err := targetRepo.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err)
		res := desc.Component.Resources[0]

		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(res.Access, &localBlobAccess),
			"a ref-only Git access must become a local blob once copied")
		r.Equal(transferv1alpha1.GitLocalBlobMediaType, localBlobAccess.MediaType)

		stored := readLocalResource(t, t.Context(), targetRepo, componentName, componentVersion, res.ToIdentity())
		r.Equal(res.Digest.Value, godigest.FromBytes(stored).Encoded(),
			"the stored blob must hash to the digest it was published under")
		assertGitArchiveREADME(t, stored, "second\n")
	})
}
