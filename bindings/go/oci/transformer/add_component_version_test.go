package transformer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// carrierTargetRepo implements both ComponentVersionRepository and
// repository.ComponentSignatureCarrier. It records whether AddComponentVersion
// and CarryComponentSignatures were invoked and with which arguments.
type carrierTargetRepo struct {
	repository.ComponentVersionRepository

	addedDescriptor *descriptor.Descriptor

	carryCalled    bool
	carrySource    repository.ComponentVersionRepository
	carryComponent string
	carryVersion   string
}

func (m *carrierTargetRepo) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	m.addedDescriptor = desc
	return nil
}

func (m *carrierTargetRepo) CarryComponentSignatures(ctx context.Context, source repository.ComponentVersionRepository, component, version string) error {
	m.carryCalled = true
	m.carrySource = source
	m.carryComponent = component
	m.carryVersion = version
	return nil
}

// sourceStubRepo is a minimal source repository stub returned by the provider
// when opening the source spec for signature carrying.
type sourceStubRepo struct {
	repository.ComponentVersionRepository
}

// carrierRepoProvider returns the target repo for the target spec and the source
// stub for the source spec, distinguishing them by BaseUrl.
type carrierRepoProvider struct {
	target        *carrierTargetRepo
	source        *sourceStubRepo
	sourceBaseURL string
}

func (m *carrierRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *carrierRepoProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials runtime.Typed) (repository.ComponentVersionRepository, error) {
	// The source spec is threaded as a *runtime.Raw; the target spec is a concrete
	// *oci.Repository. Route based on type to return the appropriate stub.
	if raw, ok := repositorySpecification.(*runtime.Raw); ok {
		_ = raw
		return m.source, nil
	}
	return m.target, nil
}

func (m *carrierRepoProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func newAddComponentVersionScheme() *runtime.Scheme {
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddComponentVersion{}, v1alpha1.OCIAddComponentVersionV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddComponentVersion{}, v1alpha1.CTFAddComponentVersionV1alpha1)
	return combinedScheme
}

func newTestV2Descriptor(name, version string) *v2.Descriptor {
	return &v2.Descriptor{
		Meta: v2.Meta{Version: "v2"},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: "ocm.software",
		},
	}
}

// TestAddComponentVersion_Transform_CarriesSignaturesWhenSourceSet verifies that
// when the upload step carries a SourceRepository spec, the transformer invokes
// CarryComponentSignatures on the target (which implements
// repository.ComponentSignatureCarrier) with the correct component/version and a
// non-nil source repository.
func TestAddComponentVersion_Transform_CarriesSignaturesWhenSourceSet(t *testing.T) {
	ctx := context.Background()

	target := &carrierTargetRepo{}
	source := &sourceStubRepo{}
	provider := &carrierRepoProvider{target: target, source: source, sourceBaseURL: "ghcr.io/source"}

	transformer := &AddComponentVersion{
		Scheme:       newAddComponentVersionScheme(),
		RepoProvider: provider,
	}

	sourceSpec := &runtime.Raw{
		Type: runtime.NewVersionedType(ocispec.Type, "v1"),
		Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`),
	}

	step := &v1alpha1.OCIAddComponentVersion{
		Type: runtime.NewVersionedType(v1alpha1.OCIAddComponentVersionType, v1alpha1.Version),
		ID:   "test-upload",
		Spec: &v1alpha1.OCIAddComponentVersionSpec{
			Repository: ocispec.Repository{
				Type:    runtime.NewVersionedType(ocispec.Type, "v1"),
				BaseUrl: "ghcr.io/target",
			},
			Descriptor:       newTestV2Descriptor("ocm.software/test-component", "1.0.0"),
			SourceRepository: sourceSpec,
		},
	}

	result, err := transformer.Transform(ctx, step)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The component version must have been added to the target.
	require.NotNil(t, target.addedDescriptor)
	assert.Equal(t, "ocm.software/test-component", target.addedDescriptor.Component.Name)
	assert.Equal(t, "1.0.0", target.addedDescriptor.Component.Version)

	// The carrier must have been invoked with the correct component/version and a
	// non-nil source repository.
	assert.True(t, target.carryCalled, "CarryComponentSignatures should have been called")
	assert.Equal(t, "ocm.software/test-component", target.carryComponent)
	assert.Equal(t, "1.0.0", target.carryVersion)
	require.NotNil(t, target.carrySource, "carry source repository must be non-nil")
	assert.Same(t, source, target.carrySource, "carry source must be the source stub returned by the provider")
}

// TestAddComponentVersion_Transform_NoCarryWhenSourceNil verifies that when the
// upload step does NOT carry a SourceRepository spec, the carrier is not invoked.
func TestAddComponentVersion_Transform_NoCarryWhenSourceNil(t *testing.T) {
	ctx := context.Background()

	target := &carrierTargetRepo{}
	source := &sourceStubRepo{}
	provider := &carrierRepoProvider{target: target, source: source}

	transformer := &AddComponentVersion{
		Scheme:       newAddComponentVersionScheme(),
		RepoProvider: provider,
	}

	step := &v1alpha1.OCIAddComponentVersion{
		Type: runtime.NewVersionedType(v1alpha1.OCIAddComponentVersionType, v1alpha1.Version),
		ID:   "test-upload",
		Spec: &v1alpha1.OCIAddComponentVersionSpec{
			Repository: ocispec.Repository{
				Type:    runtime.NewVersionedType(ocispec.Type, "v1"),
				BaseUrl: "ghcr.io/target",
			},
			Descriptor: newTestV2Descriptor("ocm.software/test-component", "1.0.0"),
			// SourceRepository intentionally left nil.
		},
	}

	result, err := transformer.Transform(ctx, step)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, target.addedDescriptor)
	assert.False(t, target.carryCalled, "CarryComponentSignatures must NOT be called when SourceRepository is nil")
}
