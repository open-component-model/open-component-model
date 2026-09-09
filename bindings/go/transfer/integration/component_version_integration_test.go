package integration_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

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

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
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
	resourceRepo := ociresource.NewResourceRepository(nil)

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

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 3. Build and execute the graph (no credentials needed for CTF-to-CTF).
	ctx := t.Context()
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{
				{Component: component1Name, Version: component1Version},
				{Component: component2Name, Version: component2Version},
			},
			Target:   targetSpec,
			Resolver: transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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

	// 3. Build the transfer graph using NewRepositoryResolver instead of a full resolver.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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
		&transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: parentName, Version: parentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// 4. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
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
