package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

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
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
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
	resourceRepo := ociresource.NewResourceRepository(nil)
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

	// Verify the resource was stored as a localBlob in the target registry.
	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess, "resource access should not be nil")
	r.Equal(descriptorv2.LocalBlobAccessType, gotAccess.GetType().Name,
		"OCI image resource should be stored as localBlob in target after CopyModeAllResources transfer")

	// Verify GlobalAccess is not set — transfer should produce a pure local blob without global access.
	accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	descriptorv2.MustAddToScheme(accessScheme)
	var typedLocalBlob descriptorv2.LocalBlob
	r.NoError(accessScheme.Convert(gotAccess, &typedLocalBlob), "should convert access to LocalBlob")
	r.Nil(typedLocalBlob.GlobalAccess, "localBlob should not have globalAccess after transfer")

	// Verify the blob is actually present and readable in the target repository.
	resourceIdentity := gotDesc.Component.Resources[0].ToIdentity()
	localBlob, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, resourceIdentity)
	r.NoError(err, "local blob should be retrievable from target repository")
	reader, err := localBlob.ReadCloser()
	r.NoError(err, "local blob should be readable")
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.NotEmpty(content, "local blob content should not be empty")
}

// Test_Integration_TransferOCIArtifact_OCIToOCI verifies the streaming TransferOCIArtifact path:
// an OCIImage resource is transferred directly from one OCI registry to another without
// intermediate tar materialisation. The resource access in the target descriptor must
// be an OCI image reference, not a local blob.
func Test_Integration_TransferOCIArtifact_OCIToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start source and target OCI registries.
	sourceAddr, sourceUser, sourcePwd := startRegistry(t)
	targetAddr, targetUser, targetPwd := startRegistry(t)

	// 2. Push a test OCI image into the source registry.
	imageRef := pushTestOCIImage(t, sourceAddr, sourceUser, sourcePwd, "test/image", "v1")

	// 3. Build a component with an external OCIImage resource pointing at that image
	//    and push it to a source OCI registry (not a CTF).
	componentName := "ocm.software/streaming-transfer-test"
	componentVersion := "1.0.0"

	credResolver := newCredResolver(t,
		registryCreds{sourceAddr, sourceUser, sourcePwd},
		registryCreds{targetAddr, targetUser, targetPwd},
	)

	// Push the component descriptor to the source OCI registry.
	sourceSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", sourceAddr),
	}
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)

	// Seed the source registry via the transfer builder itself.
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)
	ctfSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
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
						ObjectMeta: descriptor.ObjectMeta{Name: "streamed-image", Version: "1.0.0"},
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

	// CTF → source OCI (seed).
	seedTGD, err := transfer.BuildGraphDefinition(t.Context(), nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     sourceSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, ctfSpec),
		},
	)
	r.NoError(err)
	seedGraph, err := b.BuildAndCheck(seedTGD)
	r.NoError(err)
	r.NoError(seedGraph.Process(t.Context()))

	// 4. Transfer OCI → OCI with UploadAsOciArtifact (streaming path).
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", targetAddr),
	}

	// Build a live repo client for the source OCI registry so it can be used as FromRepository.
	sourceClient := createAuthClient(sourceAddr, sourceUser, sourcePwd)
	sourceURLRes, err := urlresolver.New(
		urlresolver.WithBaseURL(sourceAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(sourceClient),
	)
	r.NoError(err)
	sourceRepo, err := oci.NewRepository(oci.WithResolver(sourceURLRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{
			CopyMode:   transferv1alpha1.CopyModeAllResources,
			UploadType: transferv1alpha1.UploadAsOciArtifact,
		},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
		},
	)
	r.NoError(err)

	// Verify the graph contains a TransferOCIArtifact node, not GetOCIArtifact.
	hasTransfer := false
	for _, tr := range tgd.Transformations {
		if tr.Type.Name == "TransferOCIArtifact" {
			hasTransfer = true
		}
		r.NotEqual("GetOCIArtifact", tr.Type.Name,
			"streaming path must not emit GetOCIArtifact")
	}
	r.True(hasTransfer, "UploadAsOciArtifact OCI→OCI must emit TransferOCIArtifact")

	// Execute the graph.
	streamGraph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(streamGraph.Process(t.Context()))

	// 5. Verify the resource in the target has an OCI image reference (not a local blob).
	client := createAuthClient(targetAddr, targetUser, targetPwd)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(targetAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(t.Context(), componentName, componentVersion)
	r.NoError(err, "component must be present in target registry")
	r.Len(gotDesc.Component.Resources, 1)

	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess)

	// Streaming transfer must produce an OCI image reference in the target, not a local blob.
	r.Equal(ociaccessv1.LegacyType, gotAccess.GetType().Name,
		"resource access must be OCIImage (not local blob) after streaming OCI-to-OCI transfer")

	// Unmarshal the raw access to verify the image reference points at the target registry.
	rawAccess, ok := gotAccess.(*runtime.Raw)
	r.True(ok, "access must be a *runtime.Raw")
	var ociAccess ociaccessv1.OCIImage
	r.NoError(json.Unmarshal(rawAccess.Data, &ociAccess),
		"resource access must unmarshal to OCIImage after streaming transfer")
	r.Contains(ociAccess.ImageReference, targetAddr,
		"image reference must point to the target registry")
}

// pushTestDockerManifest pushes a minimal Docker v2 manifest image to the given registry
// and returns (imageRef http://addr/repo:tag, raw manifest bytes).
func pushTestDockerManifest(t *testing.T, registryAddr, user, password, repoPath, tag string) (string, []byte) {
	t.Helper()

	const (
		dockerManifestMediaType = "application/vnd.docker.distribution.manifest.v2+json"
		dockerConfigMediaType   = "application/vnd.docker.container.image.v1+json"
		dockerLayerMediaType    = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	)

	ref := fmt.Sprintf("%s/%s:%s", registryAddr, repoPath, tag)

	layerContent := []byte("docker layer content for integration test")
	layerDesc := ocispecv1.Descriptor{
		MediaType: dockerLayerMediaType,
		Digest:    digestOf(layerContent),
		Size:      int64(len(layerContent)),
	}

	configContent := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := ocispecv1.Descriptor{
		MediaType: dockerConfigMediaType,
		Digest:    digestOf(configContent),
		Size:      int64(len(configContent)),
	}

	// Docker manifest v2 uses a different mediaType from OCI.
	type dockerManifest struct {
		SchemaVersion int                    `json:"schemaVersion"`
		MediaType     string                 `json:"mediaType"`
		Config        ocispecv1.Descriptor   `json:"config"`
		Layers        []ocispecv1.Descriptor `json:"layers"`
	}
	manifest := dockerManifest{
		SchemaVersion: 2,
		MediaType:     dockerManifestMediaType,
		Config:        configDesc,
		Layers:        []ocispecv1.Descriptor{layerDesc},
	}
	manifestContent, err := json.Marshal(manifest)
	require.NoError(t, err)

	manifestDesc := ocispecv1.Descriptor{
		MediaType: dockerManifestMediaType,
		Digest:    digestOf(manifestContent),
		Size:      int64(len(manifestContent)),
	}

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
	require.NoError(t, err, "should push Docker manifest to registry")

	return fmt.Sprintf("http://%s", ref), manifestContent
}

// Test_Integration_TransferDockerManifestLocalBlob_CTFToOCI tests that a LocalBlob resource
// with a Docker manifest media type is correctly transferred as an OCI artifact when using
// UploadAsOciArtifact mode. This is a regression test for the bug where Docker manifests
// were excluded from isOCICompliantManifest, causing them to be silently stored as local blobs.
func Test_Integration_TransferDockerManifestLocalBlob_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const dockerManifestMediaType = "application/vnd.docker.distribution.manifest.v2+json"

	// 1. Start source and target registries.
	sourceAddr, sourceUser, sourcePwd := startRegistry(t)
	targetAddr, targetUser, targetPwd := startRegistry(t)

	// 2. Push a Docker manifest image to the source registry.
	imageRef, _ := pushTestDockerManifest(t, sourceAddr, sourceUser, sourcePwd, "test/docker-image", "v1")

	// 3. Create source CTF with an OCIImage resource pointing at the Docker manifest in source registry.
	componentName := "ocm.software/docker-manifest-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "docker-image", Version: "1.0.0"},
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

	// 4. Transfer with CopyModeAllResources + UploadAsOciArtifact.
	//    The Docker manifest OCIImage resource must be correctly transferred end-to-end.
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", targetAddr),
	}

	credResolver := newCredResolver(t,
		registryCreds{sourceAddr, sourceUser, sourcePwd},
		registryCreds{targetAddr, targetUser, targetPwd},
	)

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{
			CopyMode:   transferv1alpha1.CopyModeAllResources,
			UploadType: transferv1alpha1.UploadAsOciArtifact,
		},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// With CopyModeAllResources + UploadAsOciArtifact targeting an OCI registry, the graph
	// takes the streaming path and emits TransferOCIArtifact (not GetOCIArtifact).
	hasTransferOCIArtifact := false
	for _, tr := range tgd.Transformations {
		if tr.Type.Name == "TransferOCIArtifact" {
			hasTransferOCIArtifact = true
			break
		}
	}
	r.True(hasTransferOCIArtifact, "Docker manifest OCIImage resource with CopyModeAllResources+UploadAsOciArtifact to OCI target should generate TransferOCIArtifact transformation")

	// 5. Execute the transfer.
	ctx := t.Context()
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := ociresource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 6. Verify the component exists in the target and the resource has OCIImage access pointing to the target.
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
	r.NoError(err, "transferred component should be in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Len(gotDesc.Component.Resources, 1)

	// With UploadAsOciArtifact the Docker manifest image should be stored as an OCI artifact
	// (OCIImage access) in the target, not as a local blob.
	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess)
	r.Equal(ociaccessv1.LegacyType, gotAccess.GetType().Name,
		"Docker manifest resource should be stored as OCIImage access after CopyModeAllResources+UploadAsOciArtifact transfer")

	var typedOCIAccess ociaccessv1.OCIImage
	rawAccess, err := json.Marshal(gotAccess)
	r.NoError(err)
	r.NoError(json.Unmarshal(rawAccess, &typedOCIAccess))
	r.Contains(typedOCIAccess.ImageReference, targetAddr,
		"OCIImage access should reference the target registry after transfer")
}
