package constructor

import (
	"context"
	"fmt"
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

// mockSourceInputMethod implements SourceInputMethod for testing
type mockSourceInputMethod struct {
	processedSource *descriptor.Source
	processedBlob   blob.ReadOnlyBlob
}

func (m *mockSourceInputMethod) GetInputMethodScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *mockSourceInputMethod) GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	id := runtime.Identity{}
	id.SetType(runtime.NewVersionedType("mock", "v1"))
	return id, nil
}

func (m *mockSourceInputMethod) ProcessSource(ctx context.Context, source *constructorruntime.Source, creds map[string]string) (*SourceInputMethodResult, error) {
	if m.processedSource != nil {
		return &SourceInputMethodResult{
			ProcessedSource: m.processedSource,
		}, nil
	}
	if m.processedBlob != nil {
		return &SourceInputMethodResult{
			ProcessedBlobData: m.processedBlob,
		}, nil
	}
	return nil, nil
}

// mockSourceInputMethodProvider implements SourceInputMethodProvider for testing
type mockSourceInputMethodProvider struct {
	methods map[runtime.Type]SourceInputMethod
}

func (m *mockSourceInputMethodProvider) GetSourceInputMethod(ctx context.Context, source *constructorruntime.Source) (SourceInputMethod, error) {
	if method, ok := m.methods[source.Input.GetType()]; ok {
		return method, nil
	}
	return nil, fmt.Errorf("no input method resolvable for input specification of type %s", source.Input.GetType())
}

// setupTestComponentWithSource creates a basic component constructor with a source for testing
func setupTestComponentWithSource(t *testing.T, sourceYAML string) *constructorruntime.ComponentConstructor {
	yamlData := fmt.Sprintf(`
components:
  - name: ocm.software/test-component
    version: v1.0.0
    provider:
      name: test-provider
    resources: []
    sources:
      %s
`, sourceYAML)

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	return converted
}

// verifyBasicComponentWithSource verifies the basic component properties for source tests
func verifyBasicComponentWithSource(t *testing.T, desc *descriptor.Descriptor) {
	assert.Equal(t, "ocm.software/test-component", desc.Component.Name)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Sources, 1)
}

func TestConstructWithSourceInputMethod(t *testing.T) {
	// Create a mock source input method that returns a processed source
	mockInput := &mockSourceInputMethod{
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

	// Create a mock source input method provider
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: git
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
	}
	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(t.Context())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponentWithSource(t, desc)

	// Verify the source was processed correctly
	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedSources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithSourceAccess(t *testing.T) {
	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: git
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
	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(t.Context())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponentWithSource(t, desc)

	// Verify the source was processed correctly
	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)

	// Verify the access specification
	access, ok := source.Access.(*runtime.Raw)
	require.True(t, ok, "Access should be of type raw due to conversion")
	assert.Contains(t, string(access.Data), "application/octet-stream")

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedSources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithSourceCredentialResolution(t *testing.T) {
	// Create a mock source input method that uses credentials
	mockInput := &mockSourceInputMethod{
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

	// Create a mock source input method provider
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
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

	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: git
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
		Resolver:                  mockCredProvider,
	}

	// Process the constructor
	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(t.Context())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponentWithSource(t, desc)

	// Verify the source was processed correctly
	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)

	// Verify the access specification
	access, ok := source.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", access.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedSources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)

	// Verify the credential provider was called
	assert.Equal(t, mockCredProvider.called["mock/v1"], 1)
}

func TestConstructWithSourceBlob(t *testing.T) {
	// Create a mock source input method that returns blob data
	mockInput := &mockSourceInputMethod{
		processedBlob: &mockBlob{
			mediaType: "application/octet-stream",
			data:      []byte("test source data"),
		},
	}

	// Create a mock source input method provider
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: git
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
	}
	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(t.Context())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponentWithSource(t, desc)

	// Verify the source was processed correctly
	source := desc.Component.Sources[0]
	assert.Equal(t, "test-source", source.Name)
	assert.Equal(t, "v1.0.0", source.Version)
	assert.Equal(t, "git", source.Type)
	assert.NotNil(t, source.Access)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedSources, 1)
	assert.Len(t, mockRepo.addedVersions, 1)
}

func TestConstructWithInvalidSourceInputMethodType(t *testing.T) {
	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: blob
        input:
          type: invalid/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: &mockSourceInputMethodProvider{
			methods: map[runtime.Type]SourceInputMethod{},
		},
		TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: mockRepo},
	}

	constructorInstance := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input method resolvable for input specification of type")
}

func TestConstructWithSourceMissingAccess(t *testing.T) {
	// Create a mock source input method that returns a source without access
	mockInput := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source",
					Version: "v1.0.0",
				},
			},
			// No access specified
		},
	}

	// Create a mock source input method provider
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
	}

	ctor := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := ctor.Construct(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the input method was processed, no access was present in the source")
}

func TestConstructWithSourceCredentialResolutionError(t *testing.T) {
	// Create a mock source input method that uses credentials
	mockInput := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source",
					Version: "v1.0.0",
				},
			},
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	// Create a mock source input method provider
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

	// Create a mock credential provider that always fails
	mockCredProvider := &mockCredentialProvider{
		called:      make(map[string]int),
		credentials: map[string]map[string]string{},
		fail:        true,
	}

	constructor := setupTestComponentWithSource(t, `
      - name: test-source
        version: v1.0.0
        type: blob
        input:
          type: mock/v1
`)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
		Resolver:                  mockCredProvider,
	}

	ctor := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := ctor.Construct(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error resolving credentials for source input method")
}

func TestConstructWithMultipleSources(t *testing.T) {
	// Create mock source input methods for different source types
	mockInput1 := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source-1",
					Version: "v1.0.0",
				},
			},
			Type: "git",
			Access: &v2.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	mockInput2 := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source-2",
					Version: "v1.0.0",
				},
			},
			Type: "helm",
			Access: &v2.LocalBlob{
				MediaType: "application/x-tar",
			},
		},
	}

	// Create a mock source input method provider with multiple methods
	mockProvider := &mockSourceInputMethodProvider{
		methods: map[runtime.Type]SourceInputMethod{
			runtime.NewVersionedType("mock1", "v1"): mockInput1,
			runtime.NewVersionedType("mock2", "v1"): mockInput2,
		},
	}

	// Create a component with multiple sources
	yamlData := `
components:
  - name: ocm.software/test-component
    version: v1.0.0
    provider:
      name: test-provider
    resources: []
    sources:
      - name: test-source-1
        version: v1.0.0
        type: git
        input:
          type: mock1/v1
      - name: test-source-2
        version: v1.0.0
        type: helm
        input:
          type: mock2/v1
`

	var constructor constructorv1.ComponentConstructor
	err := yaml.Unmarshal([]byte(yamlData), &constructor)
	require.NoError(t, err)

	converted := constructorruntime.ConvertToRuntimeConstructor(&constructor)

	// Create a mock target repository
	mockRepo := newMockTargetRepository()

	// Create the constructor with our mocks
	opts := Options{
		SourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:  &mockTargetRepositoryProvider{repo: mockRepo},
	}

	constructorInstance := NewDefaultConstructor(converted, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err = constructorInstance.Construct(t.Context())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	assert.Equal(t, "ocm.software/test-component", desc.Component.Name)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Sources, 2)

	// Verify the first source
	source1 := desc.Component.Sources[0]
	assert.Equal(t, "test-source-1", source1.Name)
	assert.Equal(t, "v1.0.0", source1.Version)
	assert.Equal(t, "git", source1.Type)
	assert.NotNil(t, source1.Access)
	access1, ok := source1.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", access1.MediaType)

	// Verify the second source
	source2 := desc.Component.Sources[1]
	assert.Equal(t, "test-source-2", source2.Name)
	assert.Equal(t, "v1.0.0", source2.Version)
	assert.Equal(t, "helm", source2.Type)
	assert.NotNil(t, source2.Access)
	access2, ok := source2.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/x-tar", access2.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedSources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}
