package constructor

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/dag/sync"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockInputMethod implements ResourceInputMethod for testing
type mockInputMethod struct {
	processedResource *descriptor.Resource
	processedBlob     blob.ReadOnlyBlob
}

func (m *mockInputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	id := runtime.Identity{}
	id.SetType(runtime.NewVersionedType("mock", "v1"))
	return id, nil
}

func (m *mockInputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, creds map[string]string) (*ResourceInputMethodResult, error) {
	if m.processedResource != nil {
		return &ResourceInputMethodResult{
			ProcessedResource: m.processedResource,
		}, nil
	}
	if m.processedBlob != nil {
		return &ResourceInputMethodResult{
			ProcessedBlobData: m.processedBlob,
		}, nil
	}
	return nil, nil
}

// mockInputMethodProvider implements ResourceInputMethodProvider for testing
type mockInputMethodProvider struct {
	methods map[runtime.Type]ResourceInputMethod
}

func (m *mockInputMethodProvider) GetResourceInputMethod(ctx context.Context, resource *constructorruntime.Resource) (ResourceInputMethod, error) {
	if method, ok := m.methods[resource.Input.GetType()]; ok {
		return method, nil
	}
	return nil, fmt.Errorf("no input method resolvable for input specification of type %s", resource.Input.GetType())
}

// mockResourceRepository implements ResourceRepository for testing
type mockResourceRepository struct {
	downloadData blob.ReadOnlyBlob
	fail         bool
}

func (m *mockResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	identity = runtime.Identity{}
	identity.SetType(runtime.NewVersionedType("mock", "v1"))
	return identity, nil
}

func (m *mockResourceRepository) GetCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	identity = runtime.Identity{}
	identity.SetType(runtime.NewVersionedType("mock", "v1"))
	return identity, nil
}

func (m *mockResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	if m.fail {
		return nil, fmt.Errorf("simulated download failure")
	}
	return m.downloadData, nil
}

// mockResourceRepositoryProvider implements ResourceRepositoryProvider for testing
type mockResourceRepositoryProvider struct {
	repo ResourceRepository
}

func (m *mockResourceRepositoryProvider) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (ResourceRepository, error) {
	return m.repo, nil
}

// mockAccess implements runtime.Typed for testing
type mockAccess struct {
	Type        string `json:"type"`
	MediaType   string `json:"mediaType"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
}

func (m *mockAccess) GetType() runtime.Type {
	return runtime.NewVersionedType("mock", "v1")
}

func (m *mockAccess) SetType(typ runtime.Type) {
	// No-op for testing
}

func (m *mockAccess) DeepCopyTyped() runtime.Typed {
	return &mockAccess{
		Type:        m.Type,
		MediaType:   m.MediaType,
		Reference:   m.Reference,
		Description: m.Description,
	}
}

// mockDigestProcessor implements ResourceDigestProcessor for testing
type mockDigestProcessor struct {
	processedDigest *descriptor.Digest
}

func (m *mockDigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error) {
	identity = runtime.Identity{}
	identity.SetType(runtime.NewVersionedType("mock", "v1"))
	return identity, nil
}

func (m *mockDigestProcessor) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	if m.processedDigest != nil {
		resource.Digest = m.processedDigest
	}
	return resource, nil
}

// mockDigestProcessorProvider implements ResourceDigestProcessorProvider for testing
type mockDigestProcessorProvider struct {
	processor ResourceDigestProcessor
}

func (m *mockDigestProcessorProvider) GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (ResourceDigestProcessor, error) {
	return m.processor, nil
}

// mockCredentialProvider implements CredentialProvider for testing
type mockCredentialProvider struct {
	called      map[string]int
	credentials map[string]map[string]string
	fail        bool
}

func (m *mockCredentialProvider) Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	m.called[identity.GetType().String()]++
	if m.fail {
		return nil, fmt.Errorf("simulated credential resolution failure")
	}
	return m.credentials[identity.GetType().String()], nil
}

// setupTestComponent creates a basic component constructor for testing
func setupTestComponent(t *testing.T, resourceYAML string) *constructorruntime.ComponentConstructor {
	yamlData := fmt.Sprintf(`
components:
  - name: ocm.software/test-component
    version: v1.0.0
    provider:
      name: test-provider
    resources:
      %s
    sources: []
`, resourceYAML)

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	return converted
}

func TestConstructWithMockInputMethod(t *testing.T) {
	// Create a mock input method that returns a processed resource
	mockInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	// Create a mock input method provider
	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor
	err := constructorInstance.Construct(t.Context(), constructor)
	require.NoError(t, err)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)
		assert.Len(t, d.Vertices, 1)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)

		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 1)

		assert.Equal(t, "test-resource", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.Nil(t, res[0].Access)

		return nil
	})
	require.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithResourceAccess(t *testing.T) {
	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: external
        type: blob
        access:
          type: localBlob
          mediaType: application/octet-stream
          localReference: test-ref
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: mockRepo},
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor
	err := constructorInstance.Construct(t.Context(), constructor)
	require.NoError(t, err)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)
		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 1)

		assert.Equal(t, "test-resource", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.EqualValues(t, descriptor.ExternalRelation, res[0].Relation)
		assert.NotNil(t, res[0].Access)

		// Verify the access specification
		access, ok := res[0].Access.(*runtime.Raw)
		require.True(t, ok, "Access should be of type raw due to conversion")
		assert.Contains(t, string(access.Data), "application/octet-stream")

		return nil
	})
	require.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithCredentialResolution(t *testing.T) {
	// Create a mock input method that uses credentials
	mockInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
			Relation: descriptor.LocalRelation,
		},
	}

	// Create a mock input method provider
	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	// Create a mock credential provider with test credentials
	mockCredProvider := &mockCredentialProvider{
		called: make(map[string]int),
		credentials: map[string]map[string]string{
			"mock/v1": {
				"username": "testuser",
				"password": "testpass",
			},
		},
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
		CredentialProvider:          mockCredProvider,
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor
	err := constructorInstance.Construct(t.Context(), constructor)
	require.NoError(t, err)

	// Verify the credential provider was called
	assert.Equal(t, mockCredProvider.called["mock/v1"], 1)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)
		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 1)

		assert.Equal(t, "test-resource", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.EqualValues(t, descriptor.LocalRelation, res[0].Relation)
		assert.Nil(t, res[0].Access)
		return nil
	})
	require.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithResourceByValue(t *testing.T) {
	// Create a mock blob with test data
	mockBlob := &mockBlob{
		mediaType: "application/octet-stream",
		data:      []byte("test data"),
	}

	// Create a mock resource repository
	mockRepo := &mockResourceRepository{
		downloadData: mockBlob,
	}

	// Create a mock resource repository provider
	mockRepoProvider := &mockResourceRepositoryProvider{
		repo: mockRepo,
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: external
        type: blob
        copyPolicy: byValue
        access:
          type: mock/v1
          mediaType: application/octet-stream
          reference: test-ref
          description: "This is a test resource"
`)

	// Create a mock target repository
	mockTargetRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		TargetRepositoryProvider:   &mockTargetRepositoryProvider{repo: mockTargetRepo},
		ResourceRepositoryProvider: mockRepoProvider,
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor
	err := constructorInstance.Construct(t.Context(), constructor)
	require.NoError(t, err)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)
		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 1)

		assert.Equal(t, "test-resource", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.EqualValues(t, descriptor.ExternalRelation, res[0].Relation)
		assert.NotNil(t, res[0].Access)

		return nil
	})

	require.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockTargetRepo.addedLocalResources, 1)
	assert.Len(t, mockTargetRepo.addedVersions, 1)
}

func TestConstructWithResourceDigest(t *testing.T) {
	// Create a mock digest processor
	mockProcessor := &mockDigestProcessor{
		processedDigest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "jsonNormalisationV1",
			Value:                  "test-digest-value",
		},
	}

	// Create a mock digest processor provider
	mockDigestProvider := &mockDigestProcessorProvider{
		processor: mockProcessor,
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: external
        type: blob
        access:
          type: mock/v1
          mediaType: application/octet-stream
          reference: test-ref
          description: "This is a test resource"
`)

	// Create a mock target repository
	mockTargetRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		TargetRepositoryProvider:        &mockTargetRepositoryProvider{repo: mockTargetRepo},
		ResourceDigestProcessorProvider: mockDigestProvider,
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	err := constructorInstance.Construct(t.Context(), constructor)
	require.NoError(t, err)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)
		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 1)
		//resource := desc.Component.Resources[0]
		//assert.Equal(t, "test-resource", resource.Name)
		//assert.Equal(t, "v1.0.0", resource.Version)
		//assert.Equal(t, descriptor.ExternalRelation, resource.Relation)
		//assert.NotNil(t, resource.Access)

		assert.Equal(t, "test-resource", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.EqualValues(t, descriptor.ExternalRelation, res[0].Relation)
		assert.NotNil(t, res[0].Access)

		//// Verify the digest was processed correctly
		// TODO: provide signature in graph data
		extComp := comp.ExternalComponent
		require.NotNil(t, extComp.Signatures[0].Digest)
		assert.Equal(t, "SHA-256", extComp.Signatures[0].Digest.HashAlgorithm)
		assert.Equal(t, "jsonNormalisationV1", extComp.Signatures[0].Digest.NormalisationAlgorithm)
		assert.Equal(t, "test-digest-value", extComp.Signatures[0].Digest.Value)

		return nil
	})
	require.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockTargetRepo.addedLocalResources, 0)
	assert.Len(t, mockTargetRepo.addedVersions, 1)
}

func TestConstructWithInvalidInputMethod(t *testing.T) {
	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: invalid/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: &mockInputMethodProvider{
			methods: map[runtime.Type]ResourceInputMethod{},
		},
		TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: mockRepo},
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context(), constructor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input method resolvable for input specification of type")
}

func TestConstructWithMissingAccess(t *testing.T) {
	// Create a mock input method that returns a resource without access
	mockInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			// No access specified
		},
	}

	// Create a mock input method provider
	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context(), constructor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the input method was processed, no access was present in the resource")
}

func TestConstructWithCredentialResolutionFailure(t *testing.T) {
	// Create a mock input method that uses credentials
	mockInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	// Create a mock input method provider
	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	// Create a mock credential provider that always fails
	mockCredProvider := &mockCredentialProvider{
		called:      make(map[string]int),
		credentials: map[string]map[string]string{},
		fail:        true,
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
		CredentialProvider:          mockCredProvider,
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context(), constructor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error resolving credentials for resource input method")
}

func TestConstructWithResourceByValueFailure(t *testing.T) {
	// Create a mock resource repository that fails to download
	mockRepo := &mockResourceRepository{
		downloadData: nil,
		fail:         true,
	}

	// Create a mock resource repository provider
	mockRepoProvider := &mockResourceRepositoryProvider{
		repo: mockRepo,
	}

	constructor := setupTestComponent(t, `
      - name: test-resource
        version: v1.0.0
        relation: external
        type: blob
        copyPolicy: byValue
        access:
          type: mock/v1
          mediaType: application/octet-stream
          reference: test-ref
`)

	// Create a mock target repository
	mockTargetRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		TargetRepositoryProvider:   &mockTargetRepositoryProvider{repo: mockTargetRepo},
		ResourceRepositoryProvider: mockRepoProvider,
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context(), constructor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error downloading resource")
}

func TestConstructWithMultipleResources(t *testing.T) {
	// Create mock input methods for different resource types
	mockInput1 := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource-1",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
			Relation: descriptor.LocalRelation,
		},
	}

	mockInput2 := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource-2",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/json",
			},
			Relation: descriptor.ExternalRelation,
		},
	}

	// Create a mock input method provider with multiple methods
	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock1", "v1"): mockInput1,
			runtime.NewVersionedType("mock2", "v1"): mockInput2,
		},
	}

	// Create a component with multiple resources
	yamlData := `
components:
  - name: ocm.software/test-component
    version: v1.0.0
    provider:
      name: test-provider
    resources:
      - name: test-resource-1
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: mock1/v1
      - name: test-resource-2
        version: v1.0.0
        relation: local
        type: json
        input:
          type: mock2/v1
    sources: []
`

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
	}

	graph := sync.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(graph, opts)

	// Process the constructor
	err = constructorInstance.Construct(t.Context(), converted)
	require.NoError(t, err)

	err = graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		roots := d.Roots()
		assert.Len(t, roots, 1)
		root := roots[0]
		assert.Equal(t, "name=ocm.software/test-component,version=v1.0.0", root)

		verts := d.Vertices
		assert.Len(t, verts, 1)

		val := verts[root].Attributes[sync.AttributeValue]
		assert.NotNil(t, val)
		comp, _ := val.(*ConstructorOrExternalComponent)
		assert.NotNil(t, comp)

		res := comp.ConstructorComponent.Resources
		assert.Len(t, res, 2)

		assert.Equal(t, "test-resource-1", res[0].Name)
		assert.Equal(t, "v1.0.0", res[0].Version)
		assert.NotNil(t, res[0].Access)
		access1, ok := res[0].Access.(*v2.LocalBlob)
		require.True(t, ok, "Access should be of type LocalBlob")
		assert.Equal(t, "application/octet-stream", access1.MediaType)
		assert.Equal(t, descriptor.LocalRelation, res[0].Relation)

		assert.Equal(t, "test-resource-2", res[1].Name)
		assert.Equal(t, "v1.0.0", res[1].Version)
		assert.NotNil(t, res[1].Access)
		access2, ok := res[1].Access.(*v2.LocalBlob)
		require.True(t, ok, "Access should be of type LocalBlob")
		assert.Equal(t, "application/json", access2.MediaType)
		assert.Equal(t, descriptor.ExternalRelation, res[1].Relation)

		return nil
	})
	assert.NoError(t, err)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)

}
