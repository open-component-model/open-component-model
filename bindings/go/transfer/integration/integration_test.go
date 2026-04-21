package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	godigest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	credidentity "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/transfer"
)

const (
	distributionRegistryImage = "registry:3.0.0"
	testUsername              = "ocm"
	testPassword              = "password"
)

// --- test helpers ---

func startRegistry(t *testing.T) (address, user, password string) {
	t.Helper()

	htpasswd := generateHtpasswd(t, testUsername, testPassword)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	container, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
	)
	require.NoError(t, err, "should start registry container")

	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate registry container: %v", err)
		}
	})

	addr, err := container.HostAddress(ctx)
	require.NoError(t, err)

	return addr, testUsername, testPassword
}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, string(hash))
}

func createAuthClient(address, username, password string) *auth.Client {
	return &auth.Client{
		Client:     retry.DefaultClient,
		Credential: auth.StaticCredential(address, auth.Credential{Username: username, Password: password}),
	}
}

// registryCreds holds credentials for a single OCI registry.
type registryCreds struct {
	address  string
	username string
	password string
}

// newCredResolver creates a credentials.StaticCredentialsResolver for one or more OCI registries.
func newCredResolver(t *testing.T, registries ...registryCreds) *credentials.StaticCredentialsResolver {
	t.Helper()
	credMap := make(map[string]map[string]string)
	for _, reg := range registries {
		repo := &ocirepospec.Repository{
			Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
			BaseUrl: fmt.Sprintf("http://%s", reg.address),
		}
		identity, err := credidentity.IdentityFromOCIRepository(repo)
		require.NoError(t, err)
		credMap[identity.String()] = map[string]string{
			"username": reg.username,
			"password": reg.password,
		}
	}
	return credentials.NewStaticCredentialsResolver(credMap)
}

// createCTFRepository creates a CTF-backed OCI repository at the given path.
func createCTFRepository(t *testing.T, path string) repository.ComponentVersionRepository {
	t.Helper()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)
	repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo
}

// --- integration tests ---

func Test_Integration_TransferLocalBlob_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a component version containing a local blob resource
	componentName := "ocm.software/integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()

	ctfRepo := createCTFRepository(t, sourceCTFPath)

	resourceData := []byte("Hello, Integration Test!")
	resourceBlob := inmemory.New(bytes.NewReader(resourceData))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "test-resource", Version: "1.0.0"},
					},
					Type:     "plainText",
					Relation: descriptor.LocalRelation,
					Access: &descriptorv2.LocalBlob{
						Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
						MediaType: "text/plain",
					},
				},
			},
		},
	}

	// Add resource to CTF — the returned resource has the updated access with localReference filled in
	updatedResource, err := ctfRepo.AddLocalResource(t.Context(), componentName, componentVersion,
		&desc.Component.Resources[0], resourceBlob)
	r.NoError(err)
	desc.Component.Resources[0] = *updatedResource

	// Add component version to CTF
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 3. Build the transfer graph
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}

	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err, "graph definition should build successfully")
	r.NotNil(tgd)
	r.NotEmpty(tgd.Transformations)

	// 4. Build and execute the graph
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})

	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(t.TempDir()),
	)
	resourceRepo := resource.NewResourceRepository(nil)

	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err, "graph should build and validate")

	r.NoError(graph.Process(ctx), "graph execution should succeed")

	// 5. Verify the component exists in the target registry
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
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Equal(componentVersion, gotDesc.Component.Version)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("test-resource", gotDesc.Component.Resources[0].Name)
}

// addComponentWithResources creates a component descriptor with local blob resources,
// adds them to the repo, and returns the descriptor.
func addComponentWithResources(t *testing.T, repo repository.ComponentVersionRepository,
	name, version string, resources map[string][]byte,
) *descriptor.Descriptor {
	t.Helper()
	r := require.New(t)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
		},
	}

	for resName, data := range resources {
		res := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: resName, Version: "1.0.0"},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &descriptorv2.LocalBlob{
				Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
				MediaType: "text/plain",
			},
		}

		updatedResource, err := repo.AddLocalResource(t.Context(), name, version, &res, inmemory.New(bytes.NewReader(data)))
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *updatedResource)
	}

	r.NoError(repo.AddComponentVersion(t.Context(), desc))
	return desc
}

func Test_Integration_TransferDescriptorOnly_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a component that has NO resources.
	componentName := "ocm.software/descriptor-only"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 3. Build the transfer graph.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify the component exists in the target registry with correct metadata.
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
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Equal(componentVersion, gotDesc.Component.Version)
	r.Equal("test-provider", gotDesc.Component.Provider.Name)
	r.Empty(gotDesc.Component.Resources, "descriptor-only component should have no resources")
}

func Test_Integration_TransferMultipleResources_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a component containing 3 resources.
	componentName := "ocm.software/multi-resource"
	componentVersion := "2.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	resources := map[string][]byte{
		"resource-alpha": []byte("alpha content"),
		"resource-beta":  []byte("beta content"),
		"resource-gamma": []byte("gamma content"),
	}
	addComponentWithResources(t, ctfRepo, componentName, componentVersion, resources)

	// 3. Build the transfer graph.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify all 3 resources arrive in target.
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
	r.NoError(err, "should find transferred component in target registry")
	r.Len(gotDesc.Component.Resources, 3, "all 3 resources should be transferred")

	gotNames := make(map[string]bool)
	for _, res := range gotDesc.Component.Resources {
		gotNames[res.Name] = true
	}
	for name := range resources {
		r.True(gotNames[name], "resource %q should exist in target", name)
	}
}

func Test_Integration_TransferCTFToCTF(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Create source CTF with one component and one resource.
	componentName := "ocm.software/ctf-to-ctf"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	addComponentWithResources(t, ctfRepo, componentName, componentVersion, map[string][]byte{
		"my-resource": []byte("ctf-to-ctf data"),
	})

	// 2. Build the transfer graph with CTF target spec.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetCTFPath := t.TempDir()
	targetSpec := &ctfrepospec.Repository{
		Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath:   targetCTFPath,
		AccessMode: "readwrite|create",
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 3. Build and execute the graph (no credentials needed for CTF-to-CTF).
	ctx := t.Context()
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, nil)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 4. Verify component arrives in target CTF.
	targetRepo := createCTFRepository(t, targetCTFPath)
	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target CTF")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Equal(componentVersion, gotDesc.Component.Version)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("my-resource", gotDesc.Component.Resources[0].Name)
}

func Test_Integration_TransferMultipleComponents_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with two different components.
	component1Name := "ocm.software/multi-comp-alpha"
	component1Version := "1.0.0"
	component2Name := "ocm.software/multi-comp-beta"
	component2Version := "2.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	addComponentWithResources(t, ctfRepo, component1Name, component1Version, map[string][]byte{
		"alpha-res": []byte("alpha data"),
	})
	addComponentWithResources(t, ctfRepo, component2Name, component2Version, map[string][]byte{
		"beta-res": []byte("beta data"),
	})

	// 3. Build the transfer graph with a single WithTransfer containing two Components.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(component1Name, component1Version),
			transfer.Component(component2Name, component2Version),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify both components arrive in target.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc1, err := targetRepo.GetComponentVersion(ctx, component1Name, component1Version)
	r.NoError(err, "should find first component in target registry")
	r.Equal(component1Name, gotDesc1.Component.Name)
	r.Len(gotDesc1.Component.Resources, 1)
	r.Equal("alpha-res", gotDesc1.Component.Resources[0].Name)

	gotDesc2, err := targetRepo.GetComponentVersion(ctx, component2Name, component2Version)
	r.NoError(err, "should find second component in target registry")
	r.Equal(component2Name, gotDesc2.Component.Name)
	r.Len(gotDesc2.Component.Resources, 1)
	r.Equal("beta-res", gotDesc2.Component.Resources[0].Name)
}

func Test_Integration_TransferWithFromRepository(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a component and resource.
	componentName := "ocm.software/from-repository"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	addComponentWithResources(t, ctfRepo, componentName, componentVersion, map[string][]byte{
		"repo-resource": []byte("from-repository data"),
	})

	// 3. Build the transfer graph using FromRepository instead of FromResolver.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify the component exists in the target registry.
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
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Equal(componentVersion, gotDesc.Component.Version)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("repo-resource", gotDesc.Component.Resources[0].Name)
}

func Test_Integration_TransferRecursive_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a child component and a parent that references it.
	childName := "ocm.software/recursive-child"
	childVersion := "1.0.0"
	parentName := "ocm.software/recursive-parent"
	parentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	// Add child component (no resources).
	childDesc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    childName,
					Version: childVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), childDesc))

	// Add parent component that references the child.
	parentDesc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    parentName,
					Version: parentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			References: []descriptor.Reference{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "child-ref",
							Version: childVersion,
						},
					},
					Component: childName,
				},
			},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), parentDesc))

	// 3. Build the transfer graph with recursive enabled.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithRecursive(true),
		transfer.WithTransfer(
			transfer.Component(parentName, parentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 5. Verify both parent and child arrive in the target.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotParent, err := targetRepo.GetComponentVersion(ctx, parentName, parentVersion)
	r.NoError(err, "should find parent component in target registry")
	r.Equal(parentName, gotParent.Component.Name)
	r.Len(gotParent.Component.References, 1, "parent should have one reference")
	r.Equal(childName, gotParent.Component.References[0].Component)

	gotChild, err := targetRepo.GetComponentVersion(ctx, childName, childVersion)
	r.NoError(err, "should find child component in target registry (recursive transfer)")
	r.Equal(childName, gotChild.Component.Name)
	r.Equal(childVersion, gotChild.Component.Version)
}

// pushTestOCIImage pushes a minimal OCI image (single layer) to the given registry.
// Returns the full image reference (e.g., "localhost:5000/test/image:v1").
func pushTestOCIImage(t *testing.T, registryAddr, user, password, repoPath, tag string) string {
	t.Helper()

	ref := fmt.Sprintf("%s/%s:%s", registryAddr, repoPath, tag)
	// Return an http:// prefixed reference so the OCI client uses plain HTTP.
	httpRef := fmt.Sprintf("http://%s", ref)

	// Create a minimal OCI image: one layer + config + manifest.
	layerContent := []byte("test layer content for integration test")
	layerDesc := ocispecv1.Descriptor{
		MediaType: ocispecv1.MediaTypeImageLayer,
		Digest:    digestOf(layerContent),
		Size:      int64(len(layerContent)),
	}

	configContent := []byte("{}")
	configDesc := ocispecv1.Descriptor{
		MediaType: ocispecv1.MediaTypeImageConfig,
		Digest:    digestOf(configContent),
		Size:      int64(len(configContent)),
	}

	manifest := ocispecv1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispecv1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispecv1.Descriptor{layerDesc},
	}

	manifestContent, err := json.Marshal(manifest)
	require.NoError(t, err)

	manifestDesc := ocispecv1.Descriptor{
		MediaType: ocispecv1.MediaTypeImageManifest,
		Digest:    digestOf(manifestContent),
		Size:      int64(len(manifestContent)),
	}

	// Push to an in-memory store, then copy to the remote registry.
	store := memory.New()
	ctx := t.Context()
	require.NoError(t, store.Push(ctx, layerDesc, bytes.NewReader(layerContent)))
	require.NoError(t, store.Push(ctx, configDesc, bytes.NewReader(configContent)))
	require.NoError(t, store.Push(ctx, manifestDesc, bytes.NewReader(manifestContent)))
	require.NoError(t, store.Tag(ctx, manifestDesc, tag))

	repo, err := remote.NewRepository(ref)
	require.NoError(t, err)
	repo.PlainHTTP = true
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Credential: auth.StaticCredential(registryAddr, auth.Credential{Username: user, Password: password}),
	}

	_, err = oras.Copy(ctx, store, tag, repo, tag, oras.DefaultCopyOptions)
	require.NoError(t, err, "should push test OCI image to source registry")

	return httpRef
}

func digestOf(content []byte) godigest.Digest {
	return godigest.FromBytes(content)
}

func Test_Integration_TransferOCIImageResource_CopyModeAllResources(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start source and target OCI registries.
	sourceAddr, sourceUser, sourcePwd := startRegistry(t)
	targetAddr, targetUser, targetPwd := startRegistry(t)

	// 2. Push a test OCI image to the source registry.
	imageRef := pushTestOCIImage(t, sourceAddr, sourceUser, sourcePwd, "test/image", "v1")

	// 3. Create source CTF with a component that has an OCIImage resource
	//    pointing to the image in the source registry.
	componentName := "ocm.software/oci-resource-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "external-image", Version: "1.0.0"},
					},
					Type:     "ociImage",
					Relation: descriptor.ExternalRelation,
					Access: &ociaccessv1.OCIImage{
						Type:           runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion),
						ImageReference: imageRef,
					},
				},
			},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 4. Build the transfer graph with CopyModeAllResources.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", targetAddr),
	}

	// Credential resolver that handles both source and target registries.
	credResolver := newCredResolver(t,
		registryCreds{sourceAddr, sourceUser, sourcePwd},
		registryCreds{targetAddr, targetUser, targetPwd},
	)

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		transfer.WithCopyMode(transfer.CopyModeAllResources),
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(ctfRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotNil(tgd)

	// Verify that CopyModeAllResources generated OCI artifact transformations.
	// With CopyModeLocalBlobResources, an OCIImage resource would be skipped entirely.
	hasGetOCIArtifact := false
	for _, tr := range tgd.Transformations {
		if tr.Type.Name == "GetOCIArtifact" {
			hasGetOCIArtifact = true
			break
		}
	}
	r.True(hasGetOCIArtifact, "CopyModeAllResources should generate GetOCIArtifact transformation for OCIImage resource")

	// 5. Build and execute the graph.
	ctx := t.Context()
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := resource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 6. Verify the component arrived in the target registry with the resource.
	client := createAuthClient(targetAddr, targetUser, targetPwd)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(targetAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("external-image", gotDesc.Component.Resources[0].Name)

	// Verify the resource was stored as a localBlob with a globalAccess pointing to the target registry.
	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess, "resource access should not be nil")
	r.Equal(descriptorv2.LocalBlobAccessType, gotAccess.GetType().Name,
		"OCI image resource should be stored as localBlob in target after CopyModeAllResources transfer")

	// Convert access to LocalBlob and extract GlobalAccess.
	accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	descriptorv2.MustAddToScheme(accessScheme)
	ociaccess.MustAddToScheme(accessScheme)
	var localBlob descriptorv2.LocalBlob
	r.NoError(accessScheme.Convert(gotAccess, &localBlob), "should convert access to LocalBlob")
	r.NotNil(localBlob.GlobalAccess, "localBlob should have a globalAccess pointing to the target registry")

	// Convert GlobalAccess to OCIImage and verify it references the target registry.
	var globalOCIAccess ociaccessv1.OCIImage
	r.NoError(accessScheme.Convert(localBlob.GlobalAccess, &globalOCIAccess), "should convert globalAccess to OCIImage")
	r.Contains(globalOCIAccess.ImageReference, targetAddr,
		"globalAccess imageReference should point to the target registry")

	// Verify the image is actually resolvable in the target registry.
	globalRepo, err := remote.NewRepository(globalOCIAccess.ImageReference)
	r.NoError(err)
	globalRepo.PlainHTTP = true
	globalRepo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Credential: auth.StaticCredential(targetAddr, auth.Credential{Username: targetUser, Password: targetPwd}),
	}
	_, _, err = globalRepo.FetchReference(ctx, globalOCIAccess.ImageReference)
	r.NoError(err, "globalAccess imageReference should be resolvable in the target registry")
}
