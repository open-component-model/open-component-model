package constructor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockTargetRepository implements TargetRepository for testing
type mockTargetRepository struct {
	addedLocalResources []*descriptor.Resource
	addedSources        []*descriptor.Source
	addedVersions       []*descriptor.Descriptor
}

func (m *mockTargetRepository) GetTargetRepository(ctx context.Context, component *constructorv1.Component) (TargetRepository, error) {
	return m, nil
}

func (m *mockTargetRepository) AddLocalResource(ctx context.Context, component, version string, resource *descriptor.Resource, data blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.addedLocalResources = append(m.addedLocalResources, resource)
	return resource, nil
}

func (m *mockTargetRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, data blob.ReadOnlyBlob) (*descriptor.Source, error) {
	m.addedSources = append(m.addedSources, source)
	return source, nil
}

func (m *mockTargetRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	m.addedVersions = append(m.addedVersions, desc)
	return nil
}

// mockTargetRepositoryProvider implements TargetRepositoryProvider for testing
type mockTargetRepositoryProvider struct {
	repo TargetRepository
}

func (m *mockTargetRepositoryProvider) GetTargetRepository(ctx context.Context, component *constructorruntime.Component) (TargetRepository, error) {
	return m.repo, nil
}

// mockCredentialProviderBasic implements CredentialProvider for testing
type mockCredentialProviderBasic struct {
	called      map[string]int
	credentials map[string]map[string]string
	fail        bool
}

func (m *mockCredentialProviderBasic) Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	m.called[identity.GetType().String()]++
	if m.fail {
		return nil, fmt.Errorf("simulated credential resolution failure")
	}
	return m.credentials[identity.GetType().String()], nil
}

// mockBlob implements blob.ReadOnlyBlob for testing
type mockBlob struct {
	mediaType string
	data      []byte
}

func (m *mockBlob) Get() ([]byte, error) {
	return m.data, nil
}

func (m *mockBlob) MediaType() (string, error) {
	return m.mediaType, nil
}

func (m *mockBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestConstructWithSourceAndResource(t *testing.T) {
	// Create mock input methods for both source and resource
	mockSourceInput := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source",
					Version: "v1.0.0",
				},
			},
			Type: "git",
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	mockResourceInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/json",
			},
			Relation: descriptor.LocalRelation,
		},
	}

	// Create mock providers for both source and resource
	sourceProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockSourceInput,
		},
	}

	resourceProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockResourceInput,
		},
	}

	// Create a component with both source and resource
	yamlData := `
components:
  - name: ocm.software/test-component
    version: v1.0.0
    provider:
      name: test-provider
    resources:
      - name: test-resource
        version: v1.0.0
        relation: local
        type: json
        input:
          type: mock/v1
    sources:
      - name: test-source
        version: v1.0.0
        type: git
        input:
          type: mock/v1
`

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	// Create a mock target repository
	mockRepo := &mockTargetRepository{}

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider:   sourceProvider,
		ResourceInputMethodProvider: resourceProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
		ProcessResourceByValue: func(resource *constructorruntime.Resource) bool {
			return true
		},
	}
	constructorInstance := NewDefaultConstructor(opts)

	// Process the constructor
	descriptors, err := constructorInstance.Construct(t.Context(), converted)
	require.NoError(t, err)
	require.Len(t, descriptors, 1)

	// Verify the results
	desc := descriptors[0]
	assert.Equal(t, "ocm.software/test-component", desc.Component.Name)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Resources, 1)
	assert.Len(t, desc.Component.Sources, 1)

	// Verify the resource
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.LocalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)
	resourceAccess, ok := resource.Access.(*v2.LocalBlob)
	require.True(t, ok, "Resource access should be of type LocalBlob")
	assert.Equal(t, "application/json", resourceAccess.MediaType)

	// Verify the source
	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)
	sourceAccess, ok := source.Access.(*v2.LocalBlob)
	require.True(t, ok, "Source access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", sourceAccess.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedSources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}
