package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// --- test helpers ---

func testOCIRepo(baseURL string) *oci.Repository {
	return &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: baseURL,
	}
}

func testCTFRepo(path string) *ctfv1.Repository {
	return &ctfv1.Repository{
		Type:     runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version},
		FilePath: path,
	}
}

func testRef(component, version string, repo runtime.Typed) *compref.Ref {
	return &compref.Ref{
		Repository: repo,
		Component:  component,
		Version:    version,
	}
}

func testDescriptor(component, version string, resources []descriptor.Resource, refs []descriptor.Reference) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
			Provider:   descriptor.Provider{Name: "test-provider"},
			Resources:  resources,
			References: refs,
		},
	}
}

func testResolverFor(component, version string, repoSpec runtime.Typed, desc *descriptor.Descriptor) *mockCVRepoResolver {
	key := component + ":" + version
	return &mockCVRepoResolver{
		specs: map[string]runtime.Typed{key: repoSpec},
		repos: map[string]repository.ComponentVersionRepository{
			key: &mockCVRepo{
				descriptors: map[string]*descriptor.Descriptor{key: desc},
			},
		},
	}
}

func multiResolver(entries map[string]struct {
	spec runtime.Typed
	desc *descriptor.Descriptor
}) *mockCVRepoResolver {
	specs := make(map[string]runtime.Typed)
	allDescs := make(map[string]*descriptor.Descriptor)
	for key, entry := range entries {
		specs[key] = entry.spec
		allDescs[key] = entry.desc
	}
	sharedRepo := &mockCVRepo{descriptors: allDescs}
	repos := make(map[string]repository.ComponentVersionRepository)
	for key := range entries {
		repos[key] = sharedRepo
	}
	return &mockCVRepoResolver{specs: specs, repos: repos, sharedRepo: sharedRepo}
}

func localBlobResource(name, version string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "plainText",
		Relation: descriptor.LocalRelation,
		Access: &descriptorv2.LocalBlob{
			Type:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
			MediaType:      "text/plain",
		},
	}
}

func ociImageResource(name, version, imageRef string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "ociImage",
		Relation: descriptor.ExternalRelation,
		Access: &ociv1.OCIImage{
			Type:           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion),
			ImageReference: imageRef,
		},
	}
}

func helmResource(name, version, helmRepo, chart string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "helmChart",
		Relation: descriptor.ExternalRelation,
		Access: &helmv1.Helm{
			Type:           runtime.NewVersionedType(helmv1.LegacyType, helmv1.LegacyTypeVersion),
			HelmRepository: helmRepo,
			HelmChart:      chart,
			Version:        version,
		},
	}
}

// --- BuildGraphDefinition tests ---

func TestBuildGraphDefinition_NoResources(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeLocalBlobResources, UploadAsDefault)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	assert.NotNil(t, tgd.Environment)
	assert.Contains(t, tgd.Environment.Data, "to")
	assert.Len(t, tgd.Transformations, 1)
	assert.Contains(t, tgd.Transformations[0].ID, "Upload")
}

func TestBuildGraphDefinition_LocalBlobResource(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeLocalBlobResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 3)
	assert.Equal(t, ociv1alpha1.OCIGetLocalResourceV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)
	assert.Contains(t, tgd.Transformations[2].ID, "Upload")
}

func TestBuildGraphDefinition_OCIImageSkippedInDefaultMode(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeLocalBlobResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 1)
	assert.Contains(t, tgd.Transformations[0].ID, "Upload")
}

func TestBuildGraphDefinition_OCIImageWithCopyAllResources(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeAllResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 3)
	assert.Equal(t, ociv1alpha1.GetOCIArtifactV1alpha1, tgd.Transformations[0].Type)
}

func TestBuildGraphDefinition_OCIImageUploadAsOCIArtifact(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeAllResources, UploadAsOciArtifact)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 3)
	assert.Equal(t, ociv1alpha1.GetOCIArtifactV1alpha1, tgd.Transformations[0].Type)
	addOCIType := runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version)
	assert.Equal(t, addOCIType, tgd.Transformations[1].Type)
}

func TestBuildGraphDefinition_HelmResource(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{helmResource("my-chart", "1.0.0", "https://charts.example.com", "my-chart")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeAllResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 4)
	assert.Equal(t, helmv1alpha1.GetHelmChartV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, helmv1alpha1.ConvertHelmToOCIV1alpha1, tgd.Transformations[1].Type)
}

func TestBuildGraphDefinition_CTFTarget(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testCTFRepo("/tmp/target-archive")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	ref := testRef("ocm.software/test", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeLocalBlobResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 3)
	assert.Equal(t, ociv1alpha1.OCIGetLocalResourceV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.CTFAddLocalResourceV1alpha1, tgd.Transformations[1].Type)
	assert.Equal(t, ociv1alpha1.CTFAddComponentVersionV1alpha1, tgd.Transformations[2].Type)
}

func TestBuildGraphDefinition_Recursive(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")

	childDesc := testDescriptor("ocm.software/child", "2.0.0", nil, nil)
	rootDesc := testDescriptor("ocm.software/root", "1.0.0", nil,
		[]descriptor.Reference{{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "child-ref", Version: "2.0.0"},
			},
			Component: "ocm.software/child",
		}},
	)

	resolver := multiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/root:1.0.0":  {spec: sourceRepo, desc: rootDesc},
		"ocm.software/child:2.0.0": {spec: sourceRepo, desc: childDesc},
	})

	ref := testRef("ocm.software/root", "1.0.0", sourceRepo)

	tgd, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, true, CopyModeLocalBlobResources, UploadAsDefault)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 2)
}

func TestBuildGraphDefinition_ResolverError(t *testing.T) {
	targetRepo := testOCIRepo("ghcr.io/target")
	sourceRepo := testOCIRepo("ghcr.io/source")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	ref := testRef("ocm.software/missing", "1.0.0", sourceRepo)

	_, err := BuildGraphDefinition(t.Context(), ref, targetRepo, resolver, false, CopyModeLocalBlobResources, UploadAsDefault)
	require.Error(t, err)
}
