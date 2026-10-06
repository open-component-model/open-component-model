package integration_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	gitaccess "ocm.software/open-component-model/bindings/go/git/spec/access"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// newGitRepository creates a local repository with two commits on main and returns its path
// and the first commit. A local path is a valid Git/v1 repository, so no Git server is needed.
func newGitRepository(t *testing.T) (path string, first plumbing.Hash) {
	t.Helper()
	r := require.New(t)

	path = t.TempDir()
	repo, err := git.PlainInit(path, false)
	r.NoError(err)
	// go-git reads the global Git config; a host commit.gpgSign must not sign fixtures.
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
		if first.IsZero() {
			first = hash
		}
	}
	return path, first
}

// Transfers a Git resource pinned to a commit from a source CTF to an OCI registry, and
// checks it lands in the target as a localBlob holding the repository archive.
func Test_Integration_TransferGit_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start the target OCI registry and create the Git repository the resource is fetched from.
	registryAddr, user, password := startRegistry(t)
	repoPath, first := newGitRepository(t)

	// 2. Create a source CTF whose component has a Git resource pinned to the first commit,
	//    while main already points at the second one.
	componentName := "ocm.software/git-integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	sourceScheme := runtime.NewScheme()
	sourceScheme.MustRegisterScheme(oci.DefaultRepositoryScheme)
	// The default repository scheme knows only OCI and localBlob, and this test hand-builds a
	// typed *gitv1.Git access. The CLI constructor converts external accesses to runtime.Raw.
	gitaccess.MustAddToScheme(sourceScheme)
	ctfRepo := createCTFRepository(t, sourceCTFPath, oci.WithScheme(sourceScheme))

	sourceResource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repo-source", Version: "1.0.0"},
		},
		Type:     "directoryTree",
		Relation: descriptor.ExternalRelation,
		Access: &gitv1.Git{
			Type:       runtime.NewVersionedType(gitv1.Type, gitv1.Version),
			Repository: repoPath,
			Ref:        "refs/heads/main",
			Commit:     first.String(),
		},
	}

	// Pin the digest the way the constructor does. Without it the target would just compute a
	// digest from whatever it stored, and the check below would never fail.
	tempFolder := t.TempDir()
	resourceRepo := gitrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	digested, err := resourceRepo.ProcessResourceDigest(t.Context(), &sourceResource, nil)
	r.NoError(err)
	r.NotNil(digested.Digest, "digest processor must pin a digest")
	pinnedDigest := digested.Digest.Value

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider:  descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{*digested},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 3. Build the transfer graph with a local blob uploader (external resources are kept by reference otherwise).
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{},
		[]transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	// 4. Build and execute the graph with the Git resource repository.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))

	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify the resource landed in the target as a localBlob.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(gotDesc.Component.Resources, 1)
	res := gotDesc.Component.Resources[0]
	r.Equal("repo-source", res.Name)
	var localBlob descriptorv2.LocalBlob
	r.NoError(descriptorv2.Scheme.Convert(res.Access, &localBlob),
		"transferred Git resource must be stored as a localBlob in the target")
	r.Equal("application/x-tgz", localBlob.MediaType)

	// If the pinned digest changes, a signature over the source stops verifying against the
	// transferred component version.
	r.NotNil(res.Digest, "transferred resource should carry a digest")
	r.Equal(pinnedDigest, res.Digest.Value,
		"transferred resource must keep the digest pinned before the transfer")

	// The stored bytes must be the archive the digest was taken over.
	stored, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, res.ToIdentity())
	r.NoError(err, "local blob should be retrievable from target repository")
	reader, err := stored.ReadCloser()
	r.NoError(err, "local blob should be readable")
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(pinnedDigest, digestOf(content).Encoded(),
		"stored blob must be the exact archive the digest was taken over")
}
