package integration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Downloading the repository archive from github.com dominates both commands,
// so they get a longer budget than a local registry alone would need.
const (
	gitHubAddTimeout      = time.Minute
	gitHubTransferTimeout = time.Minute
)

// readLocalResource returns the bytes of a resource stored as a local blob.
func readLocalResource(t *testing.T, ctx context.Context, repo repository.ComponentVersionRepository, component, version string, identity ocmruntime.Identity) []byte {
	t.Helper()
	r := require.New(t)

	data, _, err := repo.GetLocalResource(ctx, component, version, identity)
	r.NoError(err, "should be able to read the transferred local blob")

	rc, err := data.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })

	raw, err := io.ReadAll(rc)
	r.NoError(err)
	return raw
}

// Test_Integration_Transfer_GitHub transfers a resource with a
// GitHub access from a CTF to an OCI registry.
//
// A source archive has no OCI-artifact representation and no digest handler for
// its media type, so it can only travel as a local blob, and its digest
// is the digest of the exact archive bytes.
//
// This test talks to live github.com — the same repository, ref and commit as
// the binding-level integration test — so it needs network access and is
// subject to GitHub's rate limits. Set GITHUB_TOKEN — or GH_TOKEN, which CI
// passes — to lift the anonymous 60/hour cap; the transfer downloads the full
// source archive.
func Test_Integration_Transfer_GitHub(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := t.Context()
	r := require.New(t)
	t.Parallel()

	// 1. Set up the target OCI registry and an ocmconfig carrying its
	//    credentials, plus the GitHub token when GITHUB_TOKEN is set.
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start target registry container")
	cfgPath := internal.CreateOCMConfigForGitHub(t, targetRegistry)

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
		componentName    = "ocm.software/test-github-component"
		componentVersion = "v1.0.0"
		resourceName     = "repo-archive"
		resourceVersion  = "v1.0.0"
		sourceName       = "repo-source"
		sourceVersion    = "v1.0.0"
	)

	// githubConstructor renders a constructor holding one github resource and one
	// github source, both pinned by pinField — "commit" or "ref".
	githubConstructor := func(repoURL, pinField, pinValue string) string {
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
      type: GitHub/v1
      repoUrl: %[7]s
      %[8]s: %[9]s
  sources:
  - name: %[5]s
    version: %[6]s
    type: directoryTree
    access:
      type: GitHub/v1
      repoUrl: %[7]s
      %[8]s: %[9]s
`, componentName, componentVersion, resourceName, resourceVersion, sourceName, sourceVersion, repoURL, pinField, pinValue)
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

		ctx, cancel := context.WithTimeout(t.Context(), gitHubAddTimeout)
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

		ctx, cancel := context.WithTimeout(t.Context(), gitHubTransferTimeout)
		defer cancel()
		return targetRef, transferCMD.ExecuteContext(ctx)
	}

	// 2. Build a source CTF with one commit-pinned github resource. Digest
	//    processing downloads the archive from github.com to hash it, giving the
	//    resource the digest the transfer must preserve.
	ctfRef, sourceCTF := addToCTF(t, githubConstructor(internal.GitHubRepoURL, "commit", internal.GitHubCommit))

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
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal(resourceName, desc.Component.Resources[0].Name)
		r.Len(desc.Component.Sources, 1)
		r.Equal(sourceName, desc.Component.Sources[0].Name)
		return desc, targetRef
	}

	t.Run("copying all resources embeds the archive as a local blob", func(t *testing.T) {
		r := require.New(t)

		desc, targetRef := transferAndFetch(t, "github-transfer-default", "--copy-resources")

		res := desc.Component.Resources[0]
		var localBlobAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(res.Access, &localBlobAccess),
			"a github access must become a local blob once copied")

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

		// The bytes must be the repository at the pinned commit, not merely some
		// archive that hashes consistently.
		archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
		r.NoError(os.WriteFile(archivePath, stored, 0o600))
		internal.AssertGitHubArchiveAtCommit(t, archivePath)

		// --copy-resources has no counterpart for sources, so the source keeps
		// pointing at github even though the resource beside it was embedded.
		// Sources carry no digest either, so nothing about them is verifiable.
		r.Equal("GitHub/v1", desc.Component.Sources[0].Access.GetType().String(),
			"--copy-resources must not embed a source")
		var sourceLocalBlobAccess v2.LocalBlob
		r.Error(v2.Scheme.Convert(desc.Component.Sources[0].Access, &sourceLocalBlobAccess),
			"a github source access must not convert to a local blob")
	})

	t.Run("the default copy mode leaves the access external", func(t *testing.T) {
		r := require.New(t)

		// A github access is not a local blob, so without --copy-resources the
		// resource content is skipped and the target keeps the remote reference.
		desc, _ := transferAndFetch(t, "github-transfer-skipped")

		res := desc.Component.Resources[0]
		r.Equal("GitHub/v1", res.Access.GetType().String(),
			"an uncopied resource must keep its github access")

		// Also proves the local-blob assertions in the sibling subtests are not
		// vacuous.
		var localBlobAccess v2.LocalBlob
		r.Error(v2.Scheme.Convert(res.Access, &localBlobAccess),
			"a github access must not convert to a local blob")
	})

	t.Run("a resource that is not pinned to a commit is refused", func(t *testing.T) {
		r := require.New(t)

		// Skip digest processing so the stored access keeps only its ref —
		// pinning it to a commit is exactly that processor's job.
		refOnlyRef, _ := addToCTF(t,
			githubConstructor(internal.GitHubRepoURL, "ref", internal.GitHubRef),
			"--skip-reference-digest-processing")

		_, err := runTransfer(t, refOnlyRef, "github-transfer-refonly", "--copy-resources")
		r.Error(err, "transferring a ref-only github resource must fail")
		r.ErrorContains(err, "no pinned commit",
			"the failure must name the missing commit, since the embedded bytes would not be reproducible")
	})
}
