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
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
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
	return nil, fmt.Errorf("component version %q not found: %w", name+":"+version, oci.ErrNotFound)
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
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider:   sourceProvider,
		ResourceInputMethodProvider: resourceProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
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
	assert.Len(t, mockRepo.components, 1)
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
