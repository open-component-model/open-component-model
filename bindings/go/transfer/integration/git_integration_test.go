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

// newGitRepository creates a local repository with two commits on main and the lightweight tag
// v1.0.0 on the first, and returns its path and the first commit. A local path is a valid Git/v1
// repository, so no Git server is needed.
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
	r.NoError(repo.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1.0.0", first)))
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
	r.Equal(transferv1alpha1.GitLocalBlobMediaType, localBlob.MediaType)
	r.Empty(localBlob.ReferenceName, "a by-value Git local blob must not carry a referenceName")

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

// Pushes a Git resource selected by the short tag name v1.0.0 with a Git uploader into an
// existing repository, directly from the source CTF and air-gapped from a CTF holding it as a
// localBlob, and reads it back from the target repository: the uploader config sets the full
// target ref, so the target gets a tag at the unchanged commit, and the published access
// downloads to the pinned digest. The uploader requires an explicit ref.
func Test_Integration_TransferGit_GitUploader(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start the target OCI registry and create the Git repository the resource is fetched
	//    from and the existing (empty) repository it is pushed into.
	registryAddr, user, password := startRegistry(t)
	repoPath, first := newGitRepository(t)
	targetGitPath := t.TempDir()
	_, err := git.PlainInit(targetGitPath, true)
	r.NoError(err)

	// 2. Create a source CTF whose component has a Git resource pinned to the first commit,
	//    with the digest the constructor would pin.
	componentName := "ocm.software/git-uploader-integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	sourceScheme := runtime.NewScheme()
	sourceScheme.MustRegisterScheme(oci.DefaultRepositoryScheme)
	gitaccess.MustAddToScheme(sourceScheme)
	ctfRepo := createCTFRepository(t, sourceCTFPath, oci.WithScheme(sourceScheme))

	tempFolder := t.TempDir()
	resourceRepo := gitrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	digested, err := resourceRepo.ProcessResourceDigest(t.Context(), &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repo-source", Version: "1.0.0"},
		},
		Type:     "directoryTree",
		Relation: descriptor.ExternalRelation,
		Access: &gitv1.Git{
			Type:       runtime.NewVersionedType(gitv1.Type, gitv1.Version),
			Repository: repoPath,
			Ref:        "v1.0.0",
			Commit:     first.String(),
		},
	}, nil)
	r.NoError(err)
	r.NotNil(digested.Digest)
	var pinnedAccess gitv1.Git
	r.NoError(gitaccess.Scheme.Convert(digested.Access, &pinnedAccess))
	r.Equal("v1.0.0", pinnedAccess.Ref, "digest processing leaves the ref as authored, it does not expand a short name")
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider:  descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{*digested},
		},
	}))
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}

	credResolver := newCredResolver(t,
		registryCreds{registryAddr + "/direct", user, password},
		registryCreds{registryAddr + "/airgap", user, password},
	)
	targetSpec := func(subPath string) *ocirepospec.Repository {
		return &ocirepospec.Repository{
			Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
			BaseUrl: fmt.Sprintf("http://%s/%s", registryAddr, subPath),
		}
	}

	// assertPushed reads the transferred component version back from subPath of the target
	// registry and the pushed commit back from the target Git repository.
	assertPushed := func(t *testing.T, subPath, repository, ref string) {
		t.Helper()
		r := require.New(t)

		urlRes, err := urlresolver.New(
			urlresolver.WithBaseURL(registryAddr+"/"+subPath),
			urlresolver.WithPlainHTTP(true),
			urlresolver.WithBaseClient(createAuthClient(registryAddr, user, password)),
		)
		r.NoError(err)
		targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
		r.NoError(err)
		gotDesc, err := targetRepo.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err)
		r.Len(gotDesc.Component.Resources, 1)
		res := gotDesc.Component.Resources[0]
		var published gitv1.Git
		r.NoError(gitaccess.Scheme.Convert(res.Access, &published), "the resource must be published with Git access")
		r.Equal(repository, published.Repository)
		r.Equal(ref, published.Ref)
		r.Equal(first.String(), published.Commit, "the commit SHA must be unchanged")
		r.NotNil(res.Digest)
		r.Equal(digested.Digest.Value, res.Digest.Value, "the pinned digest must be preserved")

		pushed, err := git.PlainOpen(repository)
		r.NoError(err)
		pushedRef, err := pushed.Reference(plumbing.ReferenceName(ref), false)
		r.NoError(err)
		r.Equal(first, pushedRef.Hash())
		commit, err := pushed.CommitObject(first)
		r.NoError(err)
		r.Equal("first\n", commit.Message, "the original commit must be pushed, not a new one")

		content, err := resourceRepo.DownloadResource(t.Context(), &res, nil)
		r.NoError(err)
		reader, err := content.ReadCloser()
		r.NoError(err)
		defer func() { r.NoError(reader.Close()) }()
		archive, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal(digested.Digest.Value, digestOf(archive).Encoded(), "the target must download to the pinned digest")
	}

	t.Run("a Git access is pushed to the configured ref", func(t *testing.T) {
		transferOnce(t, ctfRepo, sourceSpec, targetSpec("direct"),
			&transferv1alpha1.GitUploaderConfig{Repository: targetGitPath, Ref: "refs/tags/v1.0.0"}, resourceRepo, credResolver, componentName, componentVersion)
		assertPushed(t, "direct", targetGitPath, "refs/tags/v1.0.0")
	})

	t.Run("a local blob carried through an air gap is pushed with explicit match, repository and ref", func(t *testing.T) {
		r := require.New(t)

		// Stage 1, connected side: copy the resource into a CTF as a localBlob, which records
		// its origin.
		airGapPath := t.TempDir()
		airGapSpec := &ctfrepospec.Repository{
			Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
			FilePath:   airGapPath,
			AccessMode: ctfrepospec.AccessModeReadWrite + "|" + ctfrepospec.AccessModeCreate,
		}
		transferOnce(t, ctfRepo, sourceSpec, airGapSpec, &transferv1alpha1.LocalBlobUploaderConfig{}, resourceRepo, credResolver, componentName, componentVersion)
		airGapRepo := createCTFRepository(t, airGapPath)
		airGapDesc, err := airGapRepo.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err)
		var localBlob descriptorv2.LocalBlob
		r.NoError(descriptorv2.Scheme.Convert(airGapDesc.Component.Resources[0].Access, &localBlob))
		r.Equal(transferv1alpha1.GitLocalBlobMediaType, localBlob.MediaType)
		r.Empty(localBlob.ReferenceName, "a by-value Git local blob must not carry a referenceName")

		// Stage 2, air-gapped side: the local blob no longer carries its origin, so the
		// uploader needs an explicit match, repository and ref to push it.
		mirror := t.TempDir()
		_, err = git.PlainInit(mirror, true)
		r.NoError(err)
		transferOnce(t, airGapRepo, airGapSpec, targetSpec("airgap"),
			&transferv1alpha1.GitUploaderConfig{
				Match:      `resource.access.isType("LocalBlob") && resource.access.mediaType == "` + transferv1alpha1.GitLocalBlobMediaType + `"`,
				Repository: mirror,
				Ref:        "refs/tags/v1.0.0",
			},
			resourceRepo, credResolver, componentName, componentVersion)
		assertPushed(t, "airgap", mirror, "refs/tags/v1.0.0")
	})

	t.Run("the git uploader requires an explicit ref", func(t *testing.T) {
		r := require.New(t)

		old := digested.DeepCopy()
		old.Access = &gitv1.Git{
			Type:       runtime.NewVersionedType(gitv1.Type, gitv1.Version),
			Repository: repoPath,
			Ref:        "v1.0.0",
			Commit:     first.String(),
		}
		oldCTFPath := t.TempDir()
		oldRepo := createCTFRepository(t, oldCTFPath, oci.WithScheme(sourceScheme))
		r.NoError(oldRepo.AddComponentVersion(t.Context(), &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
				},
				Provider:  descriptor.Provider{Name: "test-provider"},
				Resources: []descriptor.Resource{*old},
			},
		}))
		unpushed := t.TempDir()
		_, err := git.PlainInit(unpushed, true)
		r.NoError(err)

		_, err = transfer.BuildGraphDefinition(t.Context(),
			&transferv1alpha1.Config{},
			[]transferv1alpha1.UploaderConfig{&transferv1alpha1.GitUploaderConfig{Repository: unpushed}},
			transfer.Mapping{
				Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
				Target:     targetSpec("old"),
				Resolver: transfer.NewRepositoryResolver(oldRepo, &ctfrepospec.Repository{
					Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
					FilePath: oldCTFPath,
				}),
			},
		)
		r.ErrorContains(err, "ref is required")

		target, err := git.PlainOpen(unpushed)
		r.NoError(err)
		refs, err := target.References()
		r.NoError(err)
		r.NoError(refs.ForEach(func(ref *plumbing.Reference) error {
			r.Equal(plumbing.HEAD, ref.Name(), "nothing may be pushed to the target")
			return nil
		}))
	})
}
