package constructor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"sigs.k8s.io/yaml"
)

// mockTargetRepository implements TargetRepository for testing
type mockTargetRepository struct {
	mu                     sync.Mutex
	components             map[string]*descriptor.Descriptor
	addedLocalResources    []*descriptor.Resource
	addedLocalResourceData map[string]blob.ReadOnlyBlob // resource identity -> blob data
	addedSources           []*descriptor.Source
	addedVersions          []*descriptor.Descriptor
}

func newMockTargetRepository() *mockTargetRepository {
	return &mockTargetRepository{
		components:             make(map[string]*descriptor.Descriptor),
		addedLocalResourceData: make(map[string]blob.ReadOnlyBlob),
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
	// Store the blob data so we can verify it later
	m.addedLocalResourceData[resource.ToIdentity().String()] = data
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

// componentVersionRepoProvider wraps a ComponentVersionRepository to provide it as TargetRepository
type componentVersionRepoProvider struct {
	repo repository.ComponentVersionRepository
}

func (c *componentVersionRepoProvider) GetTargetRepository(ctx context.Context, component *constructorruntime.Component) (TargetRepository, error) {
	// Wrap the ComponentVersionRepository to implement TargetRepository
	return &targetRepoWrapper{repo: c.repo}, nil
}

// targetRepoWrapper wraps a ComponentVersionRepository to implement TargetRepository
type targetRepoWrapper struct {
	repo repository.ComponentVersionRepository
}

func (t *targetRepoWrapper) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
	return t.repo.GetComponentVersion(ctx, name, version)
}

func (t *targetRepoWrapper) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	return t.repo.AddComponentVersion(ctx, desc)
}

func (t *targetRepoWrapper) AddLocalResource(ctx context.Context, component, version string, resource *descriptor.Resource, data blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return t.repo.AddLocalResource(ctx, component, version, resource, data)
}

func (t *targetRepoWrapper) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, data blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return t.repo.AddLocalSource(ctx, component, version, source, data)
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
	t.Parallel()

	// Mock source input method - returns blob data instead of processed source
	mockSourceInput := &mockSourceInputMethod{
		processedBlob: &mockBlob{
			mediaType: "application/octet-stream",
			data:      []byte("test source data"),
		},
	}

	// Mock resource input method - returns blob data instead of processed resource
	mockResourceInput := &mockInputMethod{
		processedBlob: &mockBlob{
			mediaType: "application/json",
			data:      []byte(`{"test": "resource"}`),
		},
	}

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

	// Example component structure:
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
	require.NoError(t, yaml.Unmarshal([]byte(yamlData), &constructor))
	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	// Create an actual OCI repository for external components
	externalRepo, err := ocirepository.NewFromCTFRepoV1(t.Context(), &ctf.Repository{
		FilePath:   t.TempDir(),
		AccessMode: ctf.AccessModeReadWrite,
	})
	require.NoError(t, err)

	externalDescriptor := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component-external-ref-a",
					Version: "v1.0.0",
				},
			},
			Provider: descriptor.Provider{Name: "external-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "external-local-resource",
							Version: "v1.0.0",
						},
					},
					Type:     "ociImage",
					Relation: descriptor.LocalRelation,
					Access: &v2.LocalBlob{
						MediaType: "application/json",
					},
				},
			},
		},
	}

	// Add local resource to the external repository first (required by OCI repository)
	externalResourceData := &mockBlob{
		mediaType: "application/json",
		data:      []byte(`{"external": "resource data"}`),
	}

	// Add the local resource BEFORE adding the component version
	// The AddLocalResource call updates the resource descriptor with storage information
	updatedResource, err := externalRepo.AddLocalResource(t.Context(),
		externalDescriptor.Component.Name,
		externalDescriptor.Component.Version,
		&externalDescriptor.Component.Resources[0],
		externalResourceData)
	require.NoError(t, err)

	// Update the descriptor with the modified resource (which now has the correct LocalBlob access)
	externalDescriptor.Component.Resources[0] = *updatedResource

	// Now add the component version (this will validate that the local blob exists)
	err = externalRepo.AddComponentVersion(t.Context(), externalDescriptor)
	require.NoError(t, err)

	runAssertions := func(t *testing.T, descMap map[string]*descriptor.Descriptor) {
		t.Helper()

		// ocm.software/test-component
		desc := descMap["ocm.software/test-component"]
		require.NotNil(t, desc)
		assert.Equal(t, "test-provider", desc.Component.Provider.Name)
		assert.Len(t, desc.Component.Resources, 1)
		assert.Len(t, desc.Component.Sources, 1)
		// Check that resources have proper access (LocalBlob when using blob data from input methods)
		assert.NotNil(t, desc.Component.Resources[0].Access, "resource should have access")
		assert.NotNil(t, desc.Component.Sources[0].Access, "source should have access")

		// ocm.software/test-component-ref-a
		descA := descMap["ocm.software/test-component-ref-a"]
		require.NotNil(t, descA)
		assert.Equal(t, "test-provider", descA.Component.Provider.Name)
		assert.Len(t, descA.Component.Resources, 1)
		assert.Len(t, descA.Component.Sources, 1)
		assert.NotNil(t, descA.Component.Resources[0].Access, "resource should have access")
		assert.NotNil(t, descA.Component.Sources[0].Access, "source should have access")

		// ocm.software/test-component-ref-b
		descB := descMap["ocm.software/test-component-ref-b"]
		require.NotNil(t, descB)
		assert.Equal(t, "test-provider", descB.Component.Provider.Name)
		assert.Len(t, descB.Component.Resources, 1)
		assert.Len(t, descB.Component.Sources, 1)
		assert.NotNil(t, descB.Component.Resources[0].Access, "resource should have access")
		assert.NotNil(t, descB.Component.Sources[0].Access, "source should have access")
	}

	t.Run("with external references", func(t *testing.T) {
		// Use mock repository to focus on testing the copying logic
		mockRepo := newMockTargetRepository()

		opts := Options{
			SourceInputMethodProvider:           sourceProvider,
			ResourceInputMethodProvider:         resourceProvider,
			TargetRepositoryProvider:            &mockTargetRepositoryProvider{repo: mockRepo},
			ExternalComponentRepositoryProvider: RepositoryAsExternalComponentVersionRepositoryProvider(externalRepo),
			ExternalComponentVersionCopyPolicy:  ExternalComponentVersionCopyPolicyCopyOrFail,
		}
		constructorInstance := NewDefaultConstructor(converted, opts)
		graph := constructorInstance.GetGraph()

		err = constructorInstance.Construct(t.Context())
		require.NoError(t, err)
		descs := collectDescriptors(t, graph)
		require.Len(t, descs, 4)

		descMap := make(map[string]*descriptor.Descriptor)
		for _, d := range descs {
			descMap[d.Component.Name] = d
		}
		runAssertions(t, descMap)

		// Verify external component was uploaded to target mock repository
		uploaded, err := mockRepo.GetComponentVersion(t.Context(), externalDescriptor.Component.Name, externalDescriptor.Component.Version)
		require.NoError(t, err, "external reference should have been uploaded")
		require.NotNil(t, uploaded)
		assert.Equal(t, externalDescriptor.Component.Name, uploaded.Component.Name)
		assert.Equal(t, externalDescriptor.Component.Version, uploaded.Component.Version)
		assert.Equal(t, "external-provider", uploaded.Component.Provider.Name)

		// Verify external component's local resources were copied
		require.Len(t, uploaded.Component.Resources, 1, "external component's resources should be copied")
		externalResource := uploaded.Component.Resources[0]
		assert.Equal(t, "external-local-resource", externalResource.Name)
		assert.Equal(t, "v1.0.0", externalResource.Version)
		assert.Equal(t, descriptor.LocalRelation, externalResource.Relation)

		// Verify that the local resource was added to the mock repository
		hasExternalResource := false
		for _, res := range mockRepo.addedLocalResources {
			if res.Name == "external-local-resource" {
				hasExternalResource = true

				// Verify the actual data was copied correctly
				copiedData, exists := mockRepo.addedLocalResourceData[res.ToIdentity().String()]
				assert.True(t, exists, "resource data should be stored")
				if exists && copiedData != nil {
					// Read the actual data from the blob
					reader, err := copiedData.ReadCloser()
					assert.NoError(t, err, "should be able to get reader for copied blob")
					if reader != nil {
						actualBytes, err := io.ReadAll(reader)
						assert.NoError(t, err, "should be able to read copied blob data")
						_ = reader.Close() // Close after reading

						expectedData := `{"external": "resource data"}`
						assert.Equal(t, expectedData, string(actualBytes), "copied resource data should match original")
					}

					// Verify media type is preserved
					if mediaTypeAware, ok := copiedData.(blob.MediaTypeAware); ok {
						mediaType, _ := mediaTypeAware.MediaType()
						assert.Equal(t, "application/json", mediaType, "media type should be preserved")
					}
				}
				break
			}
		}
		assert.True(t, hasExternalResource, "external component's local resource should be added to repository")
	})

	t.Run("skip external references", func(t *testing.T) {
		mockRepo := newMockTargetRepository()
		opts := Options{
			SourceInputMethodProvider:           sourceProvider,
			ResourceInputMethodProvider:         resourceProvider,
			TargetRepositoryProvider:            &mockTargetRepositoryProvider{repo: mockRepo},
			ExternalComponentRepositoryProvider: RepositoryAsExternalComponentVersionRepositoryProvider(externalRepo),
			ExternalComponentVersionCopyPolicy:  ExternalComponentVersionCopyPolicySkip,
		}

		constructorInstance := NewDefaultConstructor(converted, opts)
		graph := constructorInstance.GetGraph()

		err := constructorInstance.Construct(t.Context())
		require.NoError(t, err)
		descs := collectDescriptors(t, graph)
		require.Len(t, descs, 4)

		descMap := make(map[string]*descriptor.Descriptor)
		for _, d := range descs {
			descMap[d.Component.Name] = d
		}
		runAssertions(t, descMap)

		_, err = mockRepo.GetComponentVersion(t.Context(), externalDescriptor.Component.Name, externalDescriptor.Component.Version)
		assert.Error(t, err, "external component should not be uploaded when SkipExternalReferences is true")
	})
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

			compConstructor := &constructorruntime.ComponentConstructor{
				Components: make([]constructorruntime.Component, len(tt.components)),
			}
			for i, comp := range tt.components {
				compConstructor.Components[i] = *comp
			}

			constructorInstance := NewDefaultConstructor(compConstructor, opts)
			graph := constructorInstance.GetGraph()

			err := constructorInstance.Construct(t.Context())
			if tt.expectError {
				assert.Error(t, err)
			} else {
				descs := collectDescriptors(t, graph)
				assert.NoError(t, err)
				if len(tt.components) > 0 {
					assert.Len(t, descs, len(tt.components))

					// sort by name and version
					slices.SortFunc(descs, func(a, b *descriptor.Descriptor) int {
						if a.Component.Name == b.Component.Name {
							return bytes.Compare([]byte(a.Component.Version), []byte(b.Component.Version))
						}
						return bytes.Compare([]byte(a.Component.Name), []byte(b.Component.Name))
					})

					for i, component := range tt.components {
						assert.Equal(t, component.Name, descs[i].Component.Name)
						assert.Equal(t, component.Version, descs[i].Component.Version)
					}
				} else {
					assert.Empty(t, descs)
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

func collectDescriptors(t *testing.T, graph *syncdag.SyncedDirectedAcyclicGraph[string]) []*descriptor.Descriptor {
	var descs []*descriptor.Descriptor
	_ = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		for id, vert := range d.Vertices {
			val, ok := vert.Attributes[AttributeDescriptor]
			if !ok {
				t.Fatalf("no attributes found for vertex %s", id)
			}
			desc, ok := val.(*descriptor.Descriptor)
			if !ok {
				t.Fatalf("attribute value for vertex %s is not of type *descriptor.Descriptor", id)
			}
			descs = append(descs, desc)
		}
		return nil
	})
	return descs
}
