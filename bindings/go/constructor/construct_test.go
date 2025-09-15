package constructor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockTargetRepository implements TargetRepository for testing
type mockTargetRepository struct {
	mu                  sync.Mutex
	components          map[string]*descriptor.Descriptor
	addedLocalResources []*descriptor.Resource
	addedSources        []*descriptor.Source
	addedVersions       []*descriptor.Descriptor
}

func newMockTargetRepository() *mockTargetRepository {
	return &mockTargetRepository{
		components: make(map[string]*descriptor.Descriptor),
	}
}

func (m *mockTargetRepository) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + ":" + version
	if desc, exists := m.components[key]; exists {
		return desc, nil
	}
	return nil, fmt.Errorf("component version %q not found: %w", name+":"+version, repository.ErrNotFound)
}

func (m *mockTargetRepository) GetTargetRepository(ctx context.Context, component *constructorv1.Component) (TargetRepository, error) {
	return m, nil
}

func (m *mockTargetRepository) AddLocalResource(ctx context.Context, component, version string, resource *descriptor.Resource, data blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedLocalResources = append(m.addedLocalResources, resource)
	return resource, nil
}

func (m *mockTargetRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, data blob.ReadOnlyBlob) (*descriptor.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedSources = append(m.addedSources, source)
	return source, nil
}

func (m *mockTargetRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedVersions = append(m.addedVersions, desc)
	key := desc.Component.Name + ":" + desc.Component.Version
	m.components[key] = desc
	return nil
}

// mockTargetRepositoryProvider implements TargetRepositoryProvider for testing
type mockTargetRepositoryProvider struct {
	repo TargetRepository
}

func (m *mockTargetRepositoryProvider) GetTargetRepository(ctx context.Context, component *constructorruntime.Component) (TargetRepository, error) {
	return m.repo, nil
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

func TestConstructWithSourceAndResourceAndReferences(t *testing.T) {
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

	// Create a component with source and resource and references
	// The references form a diamond shaped directed acyclic graph (DAG).
	//    A
	//   / \
	//  B   C
	//   \ /
	//    D
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
    componentReferences:
      - name: test-component-ref
        version: v1.0.0
        componentName: ocm.software/test-component-ref-a
      - name: test-component-ref-2
        version: v1.0.0
        componentName: ocm.software/test-component-ref-b
  - name: ocm.software/test-component-ref-a
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
    componentReferences:
      - name: test-component-external-ref-a
        version: v1.0.0
        componentName: ocm.software/test-component-external-ref-a
  - name: ocm.software/test-component-ref-b
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
    componentReferences:
      - name: test-component-external-ref-a
        version: v1.0.0
        componentName: ocm.software/test-component-external-ref-a
`

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	externalRepo, err := ocirepository.NewFromCTFRepoV1(t.Context(), &ctf.Repository{
		Path:       t.TempDir(),
		AccessMode: ctf.AccessModeReadWrite,
	})
	assert.NoError(t, err)

	assert.NoError(t, externalRepo.AddComponentVersion(t.Context(), &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component-external-ref-a",
					Version: "v1.0.0",
				},
			},
			Provider: descriptor.Provider{
				Name: "external-provider",
			},
		},
	}))

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider:           sourceProvider,
		ResourceInputMethodProvider:         resourceProvider,
		TargetRepositoryProvider:            &mockTargetRepositoryProvider{repo: mockRepo},
		ExternalComponentRepositoryProvider: RepositoryAsExternalComponentVersionRepositoryProvider(externalRepo),
	}
	constructorInstance := NewDefaultConstructor(opts)

	// Process the constructor
	descriptors, err := constructorInstance.Construct(t.Context(), converted)
	require.NoError(t, err)
	require.Len(t, descriptors, 3)

	// Map descriptors by component name for easier assertions
	descMap := make(map[string]*descriptor.Descriptor)
	for _, desc := range descriptors {
		descMap[desc.Component.Name] = desc
	}

	// Test ocm.software/test-component
	desc := descMap["ocm.software/test-component"]
	require.NotNil(t, desc)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Resources, 1)
	assert.Len(t, desc.Component.Sources, 1)

	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.LocalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)
	resourceAccess, ok := resource.Access.(*v2.LocalBlob)
	require.True(t, ok, "Resource access should be of type LocalBlob")
	assert.Equal(t, "application/json", resourceAccess.MediaType)

	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)
	sourceAccess, ok := source.Access.(*v2.LocalBlob)
	require.True(t, ok, "Source access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", sourceAccess.MediaType)

	// Test ocm.software/test-component-ref-a
	descA := descMap["ocm.software/test-component-ref-a"]
	require.NotNil(t, descA)
	assert.Equal(t, "v1.0.0", descA.Component.Version)
	assert.Equal(t, "test-provider", descA.Component.Provider.Name)
	assert.Len(t, descA.Component.Resources, 1)
	assert.Len(t, descA.Component.Sources, 1)

	resourceA := descA.Component.Resources[0]
	assert.Equal(t, "test-resource", resourceA.Name)
	assert.Equal(t, "v1.0.0", resourceA.Version)
	assert.Equal(t, descriptor.LocalRelation, resourceA.Relation)
	assert.NotNil(t, resourceA.Access)
	resourceAccessA, ok := resourceA.Access.(*v2.LocalBlob)
	require.True(t, ok, "Resource access should be of type LocalBlob")
	assert.Equal(t, "application/json", resourceAccessA.MediaType)

	sourceA := descA.Component.Sources[0]
	assert.Equal(t, "test-source", sourceA.Name)
	assert.Equal(t, "v1.0.0", sourceA.Version)
	assert.Equal(t, "git", sourceA.Type)
	assert.NotNil(t, sourceA.Access)
	sourceAccessA, ok := sourceA.Access.(*v2.LocalBlob)
	require.True(t, ok, "Source access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", sourceAccessA.MediaType)

	// Test ocm.software/test-component-ref-b
	descB := descMap["ocm.software/test-component-ref-b"]
	require.NotNil(t, descB)
	assert.Equal(t, "v1.0.0", descB.Component.Version)
	assert.Equal(t, "test-provider", descB.Component.Provider.Name)
	assert.Len(t, descB.Component.Resources, 1)
	assert.Len(t, descB.Component.Sources, 1)

	resourceB := descB.Component.Resources[0]
	assert.Equal(t, "test-resource", resourceB.Name)
	assert.Equal(t, "v1.0.0", resourceB.Version)
	assert.Equal(t, descriptor.LocalRelation, resourceB.Relation)
	assert.NotNil(t, resourceB.Access)
	resourceAccessB, ok := resourceB.Access.(*v2.LocalBlob)
	require.True(t, ok, "Resource access should be of type LocalBlob")
	assert.Equal(t, "application/json", resourceAccessB.MediaType)

	sourceB := descB.Component.Sources[0]
	assert.Equal(t, "test-source", sourceB.Name)
	assert.Equal(t, "v1.0.0", sourceB.Version)
	assert.Equal(t, "git", sourceB.Type)
	assert.NotNil(t, sourceB.Access)
	sourceAccessB, ok := sourceB.Access.(*v2.LocalBlob)
	require.True(t, ok, "Source access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", sourceAccessB.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.components, 3)
}

func TestComponentVersionConflictPolicies(t *testing.T) {
	tests := []struct {
		name           string
		policy         ComponentVersionConflictPolicy
		existing       bool
		expectError    bool
		expectReplaced bool
		components     []*constructorruntime.Component
	}{
		{
			name:           "AbortAndFail with existing component",
			policy:         ComponentVersionConflictAbortAndFail,
			existing:       true,
			expectError:    true,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "AbortAndFail with no existing component",
			policy:         ComponentVersionConflictAbortAndFail,
			existing:       false,
			expectError:    false,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Skip with existing component",
			policy:         ComponentVersionConflictSkip,
			existing:       true,
			expectError:    false,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Skip with no existing component",
			policy:         ComponentVersionConflictSkip,
			existing:       false,
			expectError:    false,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Replace with existing component",
			policy:         ComponentVersionConflictReplace,
			existing:       true,
			expectError:    false,
			expectReplaced: true,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Replace with no existing component",
			policy:         ComponentVersionConflictReplace,
			existing:       false,
			expectError:    false,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Multiple components with different versions",
			policy:         ComponentVersionConflictReplace,
			existing:       true,
			expectError:    false,
			expectReplaced: true,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component-1",
							Version: "1.0.0",
						},
					},
				},
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component-2",
							Version: "2.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Same component different versions",
			policy:         ComponentVersionConflictReplace,
			existing:       true,
			expectError:    false,
			expectReplaced: true,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "2.0.0",
						},
					},
				},
			},
		},
		{
			name:           "Empty component list",
			policy:         ComponentVersionConflictReplace,
			existing:       false,
			expectError:    false,
			expectReplaced: false,
			components:     []*constructorruntime.Component{},
		},
		{
			name:           "Invalid component version",
			policy:         ComponentVersionConflictReplace,
			existing:       false,
			expectError:    false,
			expectReplaced: false,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component",
							Version: "", // Empty version
						},
					},
				},
			},
		},
		{
			name:           "Multiple components with mixed policies",
			policy:         ComponentVersionConflictReplace,
			existing:       true,
			expectError:    false,
			expectReplaced: true,
			components: []*constructorruntime.Component{
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component-1",
							Version: "1.0.0",
						},
					},
				},
				{
					ComponentMeta: constructorruntime.ComponentMeta{
						ObjectMeta: constructorruntime.ObjectMeta{
							Name:    "test-component-2",
							Version: "1.0.0",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockTargetRepository()
			opts := Options{
				ComponentVersionConflictPolicy: tt.policy,
				TargetRepositoryProvider:       &mockTargetRepositoryProvider{repo: repo},
			}

			if tt.existing {
				for _, component := range tt.components {
					existingDesc := &descriptor.Descriptor{
						Component: descriptor.Component{
							ComponentMeta: descriptor.ComponentMeta{
								ObjectMeta: descriptor.ObjectMeta{
									Name:    component.Name,
									Version: component.Version,
								},
							},
						},
					}
					err := repo.AddComponentVersion(t.Context(), existingDesc)
					require.NoError(t, err)
				}
			}

			constructor := NewDefaultConstructor(opts)
			compConstructor := &constructorruntime.ComponentConstructor{
				Components: make([]constructorruntime.Component, len(tt.components)),
			}
			for i, comp := range tt.components {
				compConstructor.Components[i] = *comp
			}

			descriptors, err := constructor.Construct(t.Context(), compConstructor)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, descriptors)
			} else {
				assert.NoError(t, err)
				if len(tt.components) > 0 {
					assert.Len(t, descriptors, len(tt.components))
					for i, component := range tt.components {
						assert.Equal(t, component.Name, descriptors[i].Component.Name)
						assert.Equal(t, component.Version, descriptors[i].Component.Version)
					}
				} else {
					assert.Empty(t, descriptors)
				}
			}

			if tt.expectReplaced || tt.existing && tt.policy == ComponentVersionConflictSkip {
				for _, component := range tt.components {
					desc, err := repo.GetComponentVersion(t.Context(), component.Name, component.Version)
					require.NoError(t, err)
					assert.NotNil(t, desc)
				}
			}
		})
	}
}
