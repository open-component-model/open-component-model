package internal

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	githubv1alpha1 "ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
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

// ociUploaders returns a single OCI uploader with the default image reference mapping.
func ociUploaders() []transferv1alpha1.UploaderConfig {
	return []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{}}
}

// withLocalBlobUploader appends the catch-all local blob uploader that replaces
// copyMode: allResources.
func withLocalBlobUploader(uploaders ...transferv1alpha1.UploaderConfig) []transferv1alpha1.UploaderConfig {
	return append(uploaders, &transferv1alpha1.LocalBlobUploaderConfig{})
}

func testTransferRoots(component, version string, target runtime.Typed, resolver resolvers.ComponentVersionRepositoryResolver) map[string]TransferRoot {
	key := component + ":" + version
	return map[string]TransferRoot{
		key: {
			RootComponentKey: key,
			Targets:          []runtime.Typed{target},
			SourceResolver:   resolver,
		},
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

func testMultiResolver(entries map[string]struct {
	spec runtime.Typed
	desc *descriptor.Descriptor
},
) *mockCVRepoResolver {
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

func relativeOCIReferenceResource(name, version, reference string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "ociImage",
		Relation: descriptor.LocalRelation,
		Access: &ociv1.RelativeOCIReference{
			Type:      runtime.NewUnversionedType(ociv1.RelativeOCIReferenceType),
			Reference: reference,
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

func githubResource(name, version, repoURL, commit string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "directoryTree",
		Relation: descriptor.ExternalRelation,
		Access: &githubv1.GitHub{
			Type:    runtime.NewVersionedType(githubv1.Type, githubv1.Version),
			RepoURL: repoURL,
			Commit:  commit,
		},
	}
}

// --- BuildGraphDefinition tests ---

func TestBuildGraphDefinition_NoResources(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	assert.NotNil(t, tgd.Environment)
	assert.Len(t, tgd.Transformations, 1)
	assert.Contains(t, tgd.Transformations[0].ID, "Upload")
}

func TestBuildGraphDefinition_LocalBlobResource(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	// A local blob is streamed by one fused node: no file buffer and no cleanup node.
	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, ociv1alpha1.TransferLocalBlobV1alpha1, tgd.Transformations[0].Type)
	assert.Contains(t, tgd.Transformations[1].ID, "Upload")
	assert.Equal(t, fmt.Sprintf("${%s.output.resource}", tgd.Transformations[0].ID),
		tgd.Transformations[1].Spec.Data["descriptor"].(map[string]any)["component"].(map[string]any)["resources"].([]any)[0])
}

// TestBuildGraphDefinition_RelativeOCIReference_CopiesAsLocalBlob proves the baseline copy
// of a relativeOciReference re-stamps the target access to localBlob (the relative type does
// not leak) and preserves the registry-relative reference as the referenceName so the
// artifact can be re-materialised as an OCI image later.
func TestBuildGraphDefinition_RelativeOCIReference_CopiesAsLocalBlob(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{relativeOCIReferenceResource("my-relative", "1.0.0", "ocm/value:v2.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	r.NoError(err)

	r.Equal(ociv1alpha1.OCIGetLocalResourceV1alpha1, tgd.Transformations[0].Type)
	add := tgd.Transformations[1]
	r.Equal(ociv1alpha1.OCIAddLocalResourceV1alpha1, add.Type)

	access := add.Spec.Data["resource"].(map[string]any)["access"].(map[string]any)
	typeStr := access["type"].(string)
	r.Contains(typeStr, descriptorv2.LocalBlobAccessType, "target access must be a localBlob")
	r.NotContains(typeStr, ociv1.RelativeOCIReferenceType, "relative type must not leak into the target access")
	r.Equal("ocm/value:v2.0", access["referenceName"],
		"the relative reference must be preserved verbatim as the local blob referenceName")
	_, hasReference := access["reference"]
	r.False(hasReference, "the relativeOciReference 'reference' field must not leak into the target access")
}

func TestBuildGraphDefinition_OCIImageSkippedInDefaultMode(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 1)
	assert.Contains(t, tgd.Transformations[0].ID, "Upload")
}

func TestBuildGraphDefinition_OCIImageWithLocalBlobUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 4)
	assert.Equal(t, ociv1alpha1.GetOCIArtifactV1alpha1, tgd.Transformations[0].Type)
}

func TestBuildGraphDefinition_OCIImage_OCIUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader(ociUploaders()...))
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 2)
	assert.Equal(t, ociv1alpha1.TransferOCIArtifactV1alpha1, tgd.Transformations[0].Type)
	assert.Contains(t, tgd.Transformations[0].ID, "Transfer")
	assert.Nil(t, findCleanupTransformation(tgd), "streaming OCI path should produce no FileCleanup node")
}

func TestBuildGraphDefinition_HelmResource(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{helmResource("my-chart", "1.0.0", "https://charts.example.com", "my-chart")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 5)
	assert.Equal(t, helmv1alpha1.GetHelmChartV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, helmv1alpha1.ConvertHelmToOCIV1alpha1, tgd.Transformations[1].Type)
}

func TestBuildGraphDefinition_GitHubResource(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{githubResource("my-source", "1.0.0",
			"https://github.com/open-component-model/open-component-model",
			"f58349914e3c775747dc1ee9af1bc83db4652266")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	// get -> add, plus the component Upload node and the FileCleanup node.
	require.Len(t, tgd.Transformations, 4)
	assert.Equal(t, githubv1alpha1.GetGitHubCommitV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)
	assert.Equal(t, FileCleanupVersionedType, tgd.Transformations[3].Type)
}

func TestBuildGraphDefinition_GitHubResource_OCIUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{githubResource("my-source", "1.0.0",
			"https://github.com/open-component-model/open-component-model",
			"f58349914e3c775747dc1ee9af1bc83db4652266")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader(ociUploaders()...))
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 4)
	assert.Equal(t, githubv1alpha1.GetGitHubCommitV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type,
		"a github resource must stay on the local-resource path even with an OCI uploader")
}

func TestBuildGraphDefinition_CTFTarget(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testCTFRepo("/tmp/target-archive")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, ociv1alpha1.TransferLocalBlobV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.CTFAddComponentVersionV1alpha1, tgd.Transformations[1].Type)
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

	resolver := testMultiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/root:1.0.0":  {spec: sourceRepo, desc: rootDesc},
		"ocm.software/child:2.0.0": {spec: sourceRepo, desc: childDesc},
	})

	roots := testTransferRoots("ocm.software/root", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	require.NoError(t, err)

	assert.Len(t, tgd.Transformations, 2)
}

func TestBuildGraphDefinition_ResolverError(t *testing.T) {
	targetRepo := testOCIRepo("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	roots := testTransferRoots("ocm.software/missing", "1.0.0", targetRepo, resolver)

	_, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.Error(t, err)
}

func TestBuildGraphDefinition_PinnedRoot(t *testing.T) {
	r := require.New(t)
	desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
	actual, err := signing.GenerateDigest(t.Context(), desc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	build := func(pin descriptor.Digest) error {
		roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
		root := roots["ocm.software/test:1.0.0"]
		root.ExpectedDigest = &pin
		roots["ocm.software/test:1.0.0"] = root
		_, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
		return err
	}

	r.NoError(build(*actual), "a root matching its pin transfers")
	wrong := *actual
	wrong.Value = strings.Repeat("0", len(actual.Value))
	r.ErrorContains(build(wrong), "does not match pinned digest", "a root not matching its pin fails")
	ociPin := *actual
	ociPin.NormalisationAlgorithm = ""
	r.ErrorContains(build(ociPin), "does not store component versions content-addressed", "an OCI digest pin needs a content-addressed source")
}

// gatedResolver forces one legal ordering of the concurrent graph discovery: it opens the
// repository of slowSpec only once the descriptors of all components in after have been read.
type gatedResolver struct {
	*mockCVRepoResolver
	slowSpec runtime.Typed
	read     map[string]chan struct{}
	once     map[string]*sync.Once
}

func newGatedResolver(base *mockCVRepoResolver, slowSpec runtime.Typed, after ...string) *gatedResolver {
	r := &gatedResolver{
		mockCVRepoResolver: base,
		slowSpec:           slowSpec,
		read:               make(map[string]chan struct{}, len(after)),
		once:               make(map[string]*sync.Once, len(after)),
	}
	for _, component := range after {
		r.read[component] = make(chan struct{})
		r.once[component] = &sync.Once{}
	}
	return r
}

func (r *gatedResolver) GetComponentVersionRepositoryForSpecification(ctx context.Context, spec runtime.Typed) (repository.ComponentVersionRepository, error) {
	repo, err := r.mockCVRepoResolver.GetComponentVersionRepositoryForSpecification(ctx, spec)
	if err != nil {
		return nil, err
	}
	if spec == r.slowSpec {
		for _, read := range r.read {
			select {
			case <-read:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return repo, nil
	}
	return &signallingRepo{ComponentVersionRepository: repo, resolver: r}, nil
}

// signallingRepo reports to its gatedResolver which descriptors have been read.
type signallingRepo struct {
	repository.ComponentVersionRepository
	resolver *gatedResolver
}

func (s *signallingRepo) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	desc, err := s.ComponentVersionRepository.GetComponentVersion(ctx, component, version)
	if once, ok := s.resolver.once[component]; ok {
		once.Do(func() { close(s.resolver.read[component]) })
	}
	return desc, err
}

// A pinned root is verified against its pin even when another root references it with a
// digest of its own: the reference digest must not replace the pin.
func TestBuildGraphDefinition_PinnedRootReferencedByOtherRoot(t *testing.T) {
	r := require.New(t)
	child := testDescriptor("ocm.software/child", "1.0.0", nil, nil)
	childDigest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	parent := testDescriptor("ocm.software/parent", "1.0.0", nil, []descriptor.Reference{{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
		Component:   "ocm.software/child",
		Digest:      *childDigest,
	}})

	childSpec := testOCIRepo("ghcr.io/source")
	resolver := newGatedResolver(testMultiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/parent:1.0.0": {spec: testOCIRepo("ghcr.io/source"), desc: parent},
		"ocm.software/child:1.0.0":  {spec: childSpec, desc: child},
	}), childSpec, "ocm.software/parent")
	wrongPin := *childDigest
	wrongPin.Value = strings.Repeat("0", len(childDigest.Value))

	target := testOCIRepo("ghcr.io/target")
	roots := map[string]TransferRoot{
		"ocm.software/parent:1.0.0": {RootComponentKey: "ocm.software/parent:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
		"ocm.software/child:1.0.0":  {RootComponentKey: "ocm.software/child:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver, ExpectedDigest: &wrongPin},
	}
	_, err = BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	r.ErrorContains(err, "does not match pinned digest", "the child root is checked against its pin")
}

// Parents may reference the same child with digests that are spelled differently but are all
// valid for it (another hash, the legacy normalisation alias). Each is verified against the
// child; only a digest that does not match fails, and it cannot be replaced by a later parent.
// The outcome must not depend on whether the child is resolved before or after a parent's
// references are discovered.
func TestBuildGraphDefinition_ReferenceDigestsOfSeveralParents(t *testing.T) {
	r := require.New(t)
	child := testDescriptor("ocm.software/child", "1.0.0", nil, nil)
	sha256Digest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	sha512Digest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA512.String())
	r.NoError(err)
	legacyDigest := *sha256Digest
	legacyDigest.NormalisationAlgorithm = signing.LegacyNormalisationAlgo
	wrongDigest := *sha256Digest
	wrongDigest.Value = strings.Repeat("0", len(sha256Digest.Value))

	tests := []struct {
		name    string
		a, b    descriptor.Digest
		wantErr string
	}{
		{name: "SHA-256 and SHA-512", a: *sha256Digest, b: *sha512Digest},
		{name: "legacy normalisation alias", a: legacyDigest, b: *sha256Digest},
		{name: "one parent expects another child", a: *sha256Digest, b: wrongDigest, wantErr: "digest mismatch"},
	}
	for _, tc := range tests {
		// Open the child only after both parents were read, or parent b only after the child was read.
		for slow, after := range map[string][]string{
			"child": {"ocm.software/a", "ocm.software/b"},
			"b":     {"ocm.software/a", "ocm.software/child"},
		} {
			t.Run(tc.name+"/"+slow+" resolved last", func(t *testing.T) {
				r := require.New(t)
				parent := func(name string, dig descriptor.Digest) *descriptor.Descriptor {
					return testDescriptor(name, "1.0.0", nil, []descriptor.Reference{{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
						Component:   "ocm.software/child",
						Digest:      dig,
					}})
				}
				specs := map[string]runtime.Typed{
					"a":     testOCIRepo("ghcr.io/source"),
					"b":     testOCIRepo("ghcr.io/source"),
					"child": testOCIRepo("ghcr.io/source"),
				}
				resolver := newGatedResolver(testMultiResolver(map[string]struct {
					spec runtime.Typed
					desc *descriptor.Descriptor
				}{
					"ocm.software/a:1.0.0":     {spec: specs["a"], desc: parent("ocm.software/a", tc.a)},
					"ocm.software/b:1.0.0":     {spec: specs["b"], desc: parent("ocm.software/b", tc.b)},
					"ocm.software/child:1.0.0": {spec: specs["child"], desc: child},
				}), specs[slow], after...)
				target := testOCIRepo("ghcr.io/target")
				roots := map[string]TransferRoot{
					"ocm.software/a:1.0.0": {RootComponentKey: "ocm.software/a:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
					"ocm.software/b:1.0.0": {RootComponentKey: "ocm.software/b:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
				}
				_, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
				if tc.wantErr != "" {
					r.ErrorContains(err, tc.wantErr)
					return
				}
				r.NoError(err)
			})
		}
	}
}

func TestBuildGraphDefinition_MultiTarget(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	target1 := testOCIRepo("ghcr.io/target1")
	target2 := testOCIRepo("ghcr.io/target2")
	desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)

	roots := map[string]TransferRoot{
		"ocm.software/test:1.0.0": {
			RootComponentKey: "ocm.software/test:1.0.0",
			Targets:          []runtime.Typed{target1, target2},
			SourceResolver:   resolver,
		},
	}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	// Should have 2 upload transformations (one per target)
	assert.Len(t, tgd.Transformations, 2)
	assert.Contains(t, tgd.Transformations[0].ID, "Upload")
	assert.Contains(t, tgd.Transformations[1].ID, "Upload")
	// IDs should be different (target-suffixed)
	assert.NotEqual(t, tgd.Transformations[0].ID, tgd.Transformations[1].ID)
}

func TestBuildGraphDefinition_MultipleRootsDifferentResolvers(t *testing.T) {
	sourceA := testOCIRepo("ghcr.io/source-a")
	sourceB := testOCIRepo("ghcr.io/source-b")
	targetA := testOCIRepo("ghcr.io/target-a")
	targetB := testOCIRepo("ghcr.io/target-b")

	descA := testDescriptor("ocm.software/a", "1.0.0", nil, nil)
	descB := testDescriptor("ocm.software/b", "2.0.0", nil, nil)

	resolverA := testResolverFor("ocm.software/a", "1.0.0", sourceA, descA)
	resolverB := testResolverFor("ocm.software/b", "2.0.0", sourceB, descB)

	roots := map[string]TransferRoot{
		"ocm.software/a:1.0.0": {RootComponentKey: "ocm.software/a:1.0.0", Targets: []runtime.Typed{targetA}, SourceResolver: resolverA},
		"ocm.software/b:2.0.0": {RootComponentKey: "ocm.software/b:2.0.0", Targets: []runtime.Typed{targetB}, SourceResolver: resolverB},
	}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	// Both components produce upload transformations — one per component
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations, one per component")
	assert.Len(t, tgd.Transformations, 2)
}

func TestBuildGraphDefinition_MultiTargetWithResources(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	target1 := testOCIRepo("ghcr.io/target1")
	target2 := testOCIRepo("ghcr.io/target2")

	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)

	roots := map[string]TransferRoot{
		"ocm.software/test:1.0.0": {
			RootComponentKey: "ocm.software/test:1.0.0",
			Targets:          []runtime.Typed{target1, target2},
			SourceResolver:   resolver,
		},
	}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	// With 1 resource and 2 targets: each target needs a streamed transfer + upload = 2, total 4.
	assert.Len(t, tgd.Transformations, 4)
}

func TestBuildGraphDefinition_RecursiveTargetPropagation(t *testing.T) {
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

	resolver := testMultiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/root:1.0.0":  {spec: sourceRepo, desc: rootDesc},
		"ocm.software/child:2.0.0": {spec: sourceRepo, desc: childDesc},
	})

	roots := testTransferRoots("ocm.software/root", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	require.NoError(t, err)

	// Both root and child should produce upload transformations to the same target
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations, one for root and one for child")
	assert.Len(t, tgd.Transformations, 2)
}

func TestBuildGraphDefinition_RecursiveResolverPropagation(t *testing.T) {
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

	// Single resolver that knows about both root and child
	resolver := testMultiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/root:1.0.0":  {spec: sourceRepo, desc: rootDesc},
		"ocm.software/child:2.0.0": {spec: sourceRepo, desc: childDesc},
	})

	roots := testTransferRoots("ocm.software/root", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	// Both root and child are resolved via the propagated resolver
	assert.Len(t, tgd.Transformations, 2)
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations after recursive resolver propagation")
}

// --- FileCleanup graph integration tests ---

// findCleanupTransformation returns the FileCleanup transformation from the graph, or nil.
func findCleanupTransformation(tgd *transformv1alpha1.TransformationGraphDefinition) *transformv1alpha1.GenericTransformation {
	for i := range tgd.Transformations {
		if tgd.Transformations[i].ID == "fileBufferCleanup" {
			return &tgd.Transformations[i]
		}
	}
	return nil
}

// cleanupFileExpressions extracts the files list from a cleanup transformation's spec.
func cleanupFileExpressions(t *testing.T, cleanup *transformv1alpha1.GenericTransformation) []string {
	t.Helper()
	require.NotNil(t, cleanup)
	require.NotNil(t, cleanup.Spec)

	filesRaw, ok := cleanup.Spec.Data["files"]
	require.True(t, ok, "cleanup spec should have a 'files' field")

	filesSlice, ok := filesRaw.([]any)
	require.True(t, ok, "files should be []any")

	exprs := make([]string, len(filesSlice))
	for i, f := range filesSlice {
		exprs[i], ok = f.(string)
		require.True(t, ok, "each file entry should be a string CEL expression")
	}
	return exprs
}

func TestBuildGraphDefinition_NoCleanupForStreamedLocalBlob(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{localBlobResource("my-resource", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	assert.Nil(t, findCleanupTransformation(tgd), "a streamed local blob buffers no file, so no FileCleanup node")
}

func TestBuildGraphDefinition_CleanupReferencesAddSpec_OCIArtifact(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	cleanup := findCleanupTransformation(tgd)
	require.NotNil(t, cleanup)

	exprs := cleanupFileExpressions(t, cleanup)
	require.Len(t, exprs, 1)

	assert.Contains(t, exprs[0], "Add")
	assert.Contains(t, exprs[0], ".spec.file")
	assert.NotContains(t, exprs[0], ".output.")
}

func TestBuildGraphDefinition_NoCleanupForStreamingOCIArtifact_OCIUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader(ociUploaders()...))
	require.NoError(t, err)

	// TransferOCIArtifact streams blobs directly — no temp file is ever created.
	cleanup := findCleanupTransformation(tgd)
	assert.Nil(t, cleanup, "streaming OCI path should not produce a FileCleanup node")
}

func TestBuildGraphDefinition_CleanupReferencesConvertAndAddSpec_Helm(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{helmResource("my-chart", "1.0.0", "https://charts.example.com", "my-chart")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	cleanup := findCleanupTransformation(tgd)
	require.NotNil(t, cleanup)

	exprs := cleanupFileExpressions(t, cleanup)
	require.Len(t, exprs, 3)

	// Chart file and prov file reference Convert's spec
	assert.Contains(t, exprs[0], "Convert")
	assert.Contains(t, exprs[0], ".spec.chartFile")
	assert.Contains(t, exprs[1], "Convert")
	assert.Contains(t, exprs[1], ".spec.?provFile")

	// OCI layout file references Add's spec
	assert.Contains(t, exprs[2], "Add")
	assert.Contains(t, exprs[2], ".spec.file")

	// None should reference .output (producer)
	for _, expr := range exprs {
		assert.NotContains(t, expr, ".output.",
			"expression %q must reference consumer spec, not producer output", expr)
	}
}

func TestBuildGraphDefinition_NoCleanupWhenNoResources(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0", nil, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	cleanup := findCleanupTransformation(tgd)
	assert.Nil(t, cleanup, "cleanup node should not be present when there are no resources")
}

func TestBuildGraphDefinition_NoCleanupWhenResourcesSkipped(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	// No uploader selects the OCI image resource, so it stays by reference.
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	cleanup := findCleanupTransformation(tgd)
	assert.Nil(t, cleanup, "cleanup node should not be present when all resources are skipped")
}

func TestBuildGraphDefinition_CleanupMultiTarget_AggregatesAllRefs(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	target1 := testOCIRepo("ghcr.io/target1")
	target2 := testOCIRepo("ghcr.io/target2")

	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)

	roots := map[string]TransferRoot{
		"ocm.software/test:1.0.0": {
			RootComponentKey: "ocm.software/test:1.0.0",
			Targets:          []runtime.Typed{target1, target2},
			SourceResolver:   resolver,
		},
	}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	cleanup := findCleanupTransformation(tgd)
	require.NotNil(t, cleanup)

	exprs := cleanupFileExpressions(t, cleanup)
	// 2 targets × 1 resource = 2 file refs
	assert.Len(t, exprs, 2, "should have one file ref per target")

	// Both should reference Add spec, and IDs should differ (target-suffixed)
	for _, expr := range exprs {
		assert.Contains(t, expr, ".spec.file")
	}
	assert.NotEqual(t, exprs[0], exprs[1], "multi-target refs should have different Add IDs")
}

func TestBuildDescriptorSpec_CreationTime(t *testing.T) {
	for _, tt := range []struct {
		name         string
		creationTime string
		want         any
	}{
		{
			name:         "preserved when resources are transformed",
			creationTime: "2025-07-28T11:40:51Z",
			want:         "${environment.envID.component.creationTime}",
		},
		{
			name: "empty does not produce a CEL reference",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			v2desc := &descriptorv2.Descriptor{
				Component: descriptorv2.Component{
					ComponentMeta: descriptorv2.ComponentMeta{
						CreationTime: tt.creationTime,
					},
					Resources: []descriptorv2.Resource{{}},
				},
			}

			spec := buildDescriptorSpec(v2desc, "envID", map[int]string{0: "resourceAdd"})
			specMap, ok := spec.(map[string]any)
			r.True(ok, "spec should be a map when resources are transformed")
			componentMap, ok := specMap["component"].(map[string]any)
			r.True(ok)
			r.Contains(componentMap, "creationTime")
			r.Equal(tt.want, componentMap["creationTime"])
			r.Equal([]any{"${resourceAdd.output.resource}"}, componentMap["resources"])
		})
	}
}

// Regression test for https://github.com/open-component-model/open-component-model/issues/2585:
// labels on the component descriptor must be forwarded into the upload transformation spec.
func TestBuildDescriptorSpec_LabelsIncluded(t *testing.T) {
	labels := []descriptorv2.Label{
		{Name: "imagevector.gardener.cloud/name", Value: []byte(`"alpine"`)},
		{Name: "priority", Value: []byte(`42`)},
	}
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "ocm.software/test",
					Version: "1.0.0",
					Labels:  labels,
				},
			},
		},
	}

	// One resource so buildDescriptorSpec builds the composite map (resourceTransformIDs non-empty).
	resourceTransformIDs := map[int]string{0: "someAddTransformID"}
	spec := buildDescriptorSpec(v2desc, "envID", resourceTransformIDs)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok, "spec should be a map when resources are transformed")

	componentMap, ok := specMap["component"].(map[string]any)
	require.True(t, ok, "component field must be a map")

	labelsExpr, ok := componentMap["labels"]
	require.True(t, ok, "labels must be present in component map")
	require.NotNil(t, labelsExpr, "labels value must not be nil")
	assert.Contains(t, labelsExpr.(string), "labels", "labels expression must reference environment labels")
}

// Regression test: when labels is nil (no labels), the field is absent from the component map
// (not set to a non-nil CEL expression that would fail evaluation).
func TestBuildDescriptorSpec_NoLabelsOmitted(t *testing.T) {
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "ocm.software/test",
					Version: "1.0.0",
				},
			},
		},
	}

	resourceTransformIDs := map[int]string{0: "someAddTransformID"}
	spec := buildDescriptorSpec(v2desc, "envID", resourceTransformIDs)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok)

	componentMap, ok := specMap["component"].(map[string]any)
	require.True(t, ok)

	labelsVal, present := componentMap["labels"]
	if present {
		assert.Nil(t, labelsVal, "when labels is nil, the map entry must be nil (not a CEL ref)")
	}
}

// Sibling of TestBuildDescriptorSpec_LabelsIncluded covering the
// descriptor-level Signatures field, which buildDescriptorSpec gates with a
// separate length check from the component-level slice fields. When
// signatures are populated, the spec map must carry a CEL placeholder
// referencing the environment signatures.
func TestBuildDescriptorSpec_SignaturesIncluded(t *testing.T) {
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "ocm.software/test",
					Version: "1.0.0",
				},
			},
		},
		Signatures: []descriptorv2.Signature{
			{
				Name:      "test-sig",
				Digest:    descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: "deadbeef"},
				Signature: descriptorv2.SignatureInfo{Algorithm: "RSASSA-PKCS1-V1_5", Value: "00", MediaType: "application/vnd.ocm.signature.rsa"},
			},
		},
	}

	resourceTransformIDs := map[int]string{0: "someAddTransformID"}
	spec := buildDescriptorSpec(v2desc, "envID", resourceTransformIDs)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok, "spec should be a map when resources are transformed")

	signaturesExpr, ok := specMap["signatures"]
	require.True(t, ok, "signatures must be present in spec map when populated")
	require.NotNil(t, signaturesExpr, "signatures value must not be nil")
	assert.Contains(t, signaturesExpr.(string), "signatures",
		"signatures expression must reference environment signatures")
}

// TestBuildDescriptorSpec_EmptyOptionalFields: empty (not nil) optional
// slice fields on a component descriptor must not produce CEL placeholders.
// Regression for
// https://github.com/open-component-model/open-component-model/issues/2742.
func TestBuildDescriptorSpec_EmptyOptionalFields(t *testing.T) {
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "ocm.software/test",
					Version: "1.0.0",
					Labels:  []descriptorv2.Label{},
				},
			},
			RepositoryContexts: []*runtime.Raw{},
			Sources:            []descriptorv2.Source{},
			References:         []descriptorv2.Reference{},
		},
		Signatures: []descriptorv2.Signature{},
	}

	resourceTransformIDs := map[int]string{0: "someAddTransformID"}
	spec := buildDescriptorSpec(v2desc, "envID", resourceTransformIDs)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	componentMap, ok := specMap["component"].(map[string]any)
	require.True(t, ok)

	if val, present := componentMap["labels"]; present {
		assert.Nilf(t, val, "labels must be nil for empty slice, got %v", val)
	}
	// Serialized without omitempty, so they must stay [] and not turn into null (#3885).
	for _, field := range []string{"repositoryContexts", "sources", "componentReferences"} {
		assert.Equalf(t, []any{}, componentMap[field], "component[%q] must be a literal empty list", field)
	}
	if val, present := specMap["signatures"]; present {
		assert.Nilf(t, val, "signatures must be absent for empty slice, got %v", val)
	}
}

func TestBuildDescriptorSpec_NilListFieldsStayNil(t *testing.T) {
	r := require.New(t)
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
			},
		},
	}

	spec := buildDescriptorSpec(v2desc, "envID", map[int]string{0: "someAddTransformID"})

	specMap, ok := spec.(map[string]any)
	r.True(ok)
	componentMap, ok := specMap["component"].(map[string]any)
	r.True(ok)
	for _, field := range []string{"repositoryContexts", "sources", "componentReferences"} {
		val, present := componentMap[field]
		r.Truef(present, "component[%q] must be set", field)
		r.Nilf(val, "component[%q] must stay nil for a nil slice", field)
	}
}

func dockerManifestLocalBlobResource(name, version string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "ociImage",
		Relation: descriptor.LocalRelation,
		Access: &descriptorv2.LocalBlob{
			Type:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
			MediaType:      mediaTypeDockerManifest,
			ReferenceName:  "ghcr.io/org/image:v1",
		},
	}
}

func TestBuildGraphDefinition_DockerManifestLocalBlob_OCIUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{dockerManifestLocalBlobResource("my-image", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, ociUploaders())
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, ociv1alpha1.TransferLocalBlobToOCIArtifactV1alpha1, tgd.Transformations[0].Type,
		"Docker manifest LocalBlob should be streamed as OCI artifact")
}

func TestBuildGraphDefinition_DockerManifestLocalBlob_FallsBackToLocalBlobWithoutOCIUploader(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{dockerManifestLocalBlobResource("my-image", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, ociv1alpha1.TransferLocalBlobV1alpha1, tgd.Transformations[0].Type)
}
