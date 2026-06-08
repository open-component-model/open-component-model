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
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockInputMethod implements ResourceInputMethod for testing
type mockInputMethod struct {
	processedResource *descriptor.Resource
	processedBlob     blob.ReadOnlyBlob
	capturedCreds     runtime.Typed
}

func (m *mockInputMethod) GetInputMethodScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *mockInputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	id := runtime.Identity{}
	id.SetType(runtime.NewVersionedType("mock", "v1"))
	return id, nil
}

func (m *mockInputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, creds runtime.Typed) (*ResourceInputMethodResult, error) {
	m.capturedCreds = creds
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

// mockResourceRepository implements ResourceRepository for testing. It serves
// downloads (downloadData/fail) and records how AddOwnership was invoked
// so the constructor's by-reference ownership attach chain can be asserted.
type mockResourceRepository struct {
	downloadData blob.ReadOnlyBlob
	fail         bool

	identityErr error // returned by GetResourceCredentialConsumerIdentity
	attachErr   error // returned by AddOwnership

	attachCalls  int
	gotComponent string
	gotVersion   string
	gotCreds     runtime.Typed
}

func (m *mockResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	if m.identityErr != nil {
		return nil, m.identityErr
	}
	identity = runtime.Identity{}
	identity.SetType(runtime.NewVersionedType("mock", "v1"))
	return identity, nil
}

func (m *mockResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if m.fail {
		return nil, fmt.Errorf("simulated download failure")
	}
	return m.downloadData, nil
}

func (m *mockResourceRepository) AddOwnership(ctx context.Context, component, version string, res *descriptor.Resource, credentials runtime.Typed) error {
	m.attachCalls++
	m.gotComponent = component
	m.gotVersion = version
	m.gotCreds = credentials
	return m.attachErr
}

// mockResourceRepositoryProvider implements ResourceRepositoryProvider for testing
type mockResourceRepositoryProvider struct {
	repo ResourceRepository
}

func (m *mockResourceRepositoryProvider) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (ResourceRepository, error) {
	return m.repo, nil
}

// mockResourceRepositoryProviderWithError returns its repo and a fixed error, so the
// "could not resolve the resource repository" branch can be exercised.
type mockResourceRepositoryProviderWithError struct {
	repo ResourceRepository
	err  error
}

func (m *mockResourceRepositoryProviderWithError) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (ResourceRepository, error) {
	return m.repo, m.err
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

func (m *mockDigestProcessor) GetResourceRepositoryScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *mockDigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error) {
	identity = runtime.Identity{}
	identity.SetType(runtime.NewVersionedType("mock", "v1"))
	return identity, nil
}

func (m *mockDigestProcessor) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
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

func (m *mockCredentialProvider) Resolve(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	m.called[identity.GetType().String()]++
	if m.fail {
		return nil, fmt.Errorf("simulated credential resolution failure")
	}
	creds := m.credentials[identity.GetType().String()]
	if creds == nil {
		return nil, nil
	}
	return &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: creds,
	}, nil
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

// verifyBasicComponent verifies the basic component properties
func verifyBasicComponent(t *testing.T, desc *descriptor.Descriptor) {
	assert.Equal(t, "ocm.software/test-component", desc.Component.Name)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Resources, 1)
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
	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponent(t, desc)

	// Verify the resource was processed correctly
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.NotNil(t, resource.Access)

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
           type: LocalBlob
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
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponent(t, desc)

	// Verify the resource was processed correctly
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.ExternalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)

	// Verify the access specification
	access, ok := resource.Access.(*runtime.Raw)
	require.True(t, ok, "Access should be of type raw due to conversion")
	assert.Contains(t, string(access.Data), "application/octet-stream")

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
		Resolver:                    mockCredProvider,
	}

	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponent(t, desc)

	// Verify the resource was processed correctly
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.LocalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)

	// Verify the access specification
	access, ok := resource.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", access.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)

	// Verify the credential provider was called
	assert.Equal(t, mockCredProvider.called["mock/v1"], 1)
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

	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponent(t, desc)

	// Verify the resource was processed correctly
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.ExternalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)

	// Verify the repository was called correctly
	assert.Len(t, mockTargetRepo.addedLocalResources, 1)
	assert.Len(t, mockTargetRepo.addedVersions, 1)

	// The resource did not opt into ownership, so the by-value add must
	// not attach one: AddOwnership is never called. The opt-in lives on the runtime
	// resource options, not on descriptor.Resource.
	assert.Zero(t, mockTargetRepo.ownershipCalls, "a resource that did not opt in must not attach ownership")
}

// TestAddColocatedResourceLocalBlob_AttachesOwnershipOptIn proves the
// ADR-0016 opt-in drives a by-value AddOwnership call on the target repository
// (when it supports the capability), sourced from the runtime resource options —
// not from descriptor.Resource, which no longer carries the policy. It also
// asserts the uploaded resource and the resolved credentials are forwarded. This
// is the by-value half of the opt-in wiring; the by-reference half is covered by
// TestDefaultConstructor_attachOwnership_CallSiteGating.
func TestAddColocatedResourceLocalBlob_AttachesOwnershipOptIn(t *testing.T) {
	const (
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)
	tests := []struct {
		name      string
		policy    constructorruntime.OwnershipPolicy
		wantCalls int
	}{
		{name: "opted in (Always)", policy: constructorruntime.OwnershipPolicyAlways, wantCalls: 1},
		{name: "not opted in (Never)", policy: constructorruntime.OwnershipPolicyNever, wantCalls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockTargetRepository()
			res := &constructorruntime.Resource{
				ElementMeta: constructorruntime.ElementMeta{ObjectMeta: constructorruntime.ObjectMeta{Name: "backend-image", Version: version}},
				Type:        "ociArtifact",
				Relation:    constructorruntime.LocalRelation,
				Options:     constructorruntime.ResourceOptions{OwnershipPolicy: tt.policy},
			}
			data := &mockBlob{mediaType: "application/octet-stream", data: []byte("payload")}
			creds := &mockAccess{Type: "mock/v1"}

			out, err := addColocatedResourceLocalBlob(context.Background(), repo, component, version, res, data, creds)
			require.NoError(t, err)
			require.NotNil(t, out)

			assert.Equal(t, tt.wantCalls, repo.ownershipCalls,
				"by-value add must attach ownership iff the runtime options opt in")
			if tt.wantCalls > 0 {
				assert.Same(t, out, repo.ownershipResource, "the uploaded resource must be forwarded to AddOwnership")
				assert.Equal(t, creds, repo.ownershipCreds, "credentials must be forwarded to AddOwnership")
			}
		})
	}
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

	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()

	// Process the constructor
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.NoError(t, err)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	verifyBasicComponent(t, desc)

	// Verify the resource was processed correctly
	resource := desc.Component.Resources[0]
	assert.Equal(t, "test-resource", resource.Name)
	assert.Equal(t, "v1.0.0", resource.Version)
	assert.Equal(t, descriptor.ExternalRelation, resource.Relation)
	assert.NotNil(t, resource.Access)

	// Verify the digest was processed correctly
	require.NotNil(t, resource.Digest)
	assert.Equal(t, "SHA-256", resource.Digest.HashAlgorithm)
	assert.Equal(t, "jsonNormalisationV1", resource.Digest.NormalisationAlgorithm)
	assert.Equal(t, "test-digest-value", resource.Digest.Value)

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

	constructorInstance := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context())
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
	constructorInstance := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context())
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
		Resolver:                    mockCredProvider,
	}

	constructorInstance := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context())
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
	constructorInstance := NewDefaultConstructor(constructor, opts)

	// Process the constructor and expect an error
	err := constructorInstance.Construct(t.Context())
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
	graph := syncdag.NewSyncedDirectedAcyclicGraph[string]()
	constructorInstance := NewDefaultConstructor(converted, opts)
	graph = constructorInstance.GetGraph()

	// Process the constructor
	err = constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.Len(t, descs, 1)

	// Verify the results
	desc := descs[0]
	assert.Equal(t, "ocm.software/test-component", desc.Component.Name)
	assert.Equal(t, "v1.0.0", desc.Component.Version)
	assert.Equal(t, "test-provider", desc.Component.Provider.Name)
	assert.Len(t, desc.Component.Resources, 2)

	// Verify the first resource
	resource1 := desc.Component.Resources[0]
	assert.Equal(t, "test-resource-1", resource1.Name)
	assert.Equal(t, "v1.0.0", resource1.Version)
	assert.Equal(t, descriptor.LocalRelation, resource1.Relation)
	assert.NotNil(t, resource1.Access)
	access1, ok := resource1.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/octet-stream", access1.MediaType)

	// Verify the second resource
	resource2 := desc.Component.Resources[1]
	assert.Equal(t, "test-resource-2", resource2.Name)
	assert.Equal(t, "v1.0.0", resource2.Version)
	assert.Equal(t, descriptor.ExternalRelation, resource2.Relation)
	assert.NotNil(t, resource2.Access)
	access2, ok := resource2.Access.(*v2.LocalBlob)
	require.True(t, ok, "Access should be of type LocalBlob")
	assert.Equal(t, "application/json", access2.MediaType)

	// Verify the repository was called correctly
	assert.Len(t, mockRepo.addedLocalResources, 0)
	assert.Len(t, mockRepo.addedVersions, 1)
}

// TestConstructCredentialsPassedAsDirectCredentials verifies that credentials resolved by
// the credential provider are forwarded to ProcessResource as *credconfigv1.DirectCredentials,
// not as a raw runtime.Identity or any other type.
func TestConstructCredentialsPassedAsDirectCredentials(t *testing.T) {
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

	mockProvider := &mockInputMethodProvider{
		methods: map[runtime.Type]ResourceInputMethod{
			runtime.NewVersionedType("mock", "v1"): mockInput,
		},
	}

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

	mockRepo := newMockTargetRepository()

	opts := Options{
		ResourceInputMethodProvider: mockProvider,
		TargetRepositoryProvider:    &mockTargetRepositoryProvider{repo: mockRepo},
		Resolver:                    mockCredProvider,
	}

	constructorInstance := NewDefaultConstructor(constructor, opts)
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)

	// Credentials must arrive as *DirectCredentials so that typed credential
	// implementations (helm, oci, etc.) can inspect or convert them correctly.
	require.NotNil(t, mockInput.capturedCreds, "expected credentials to be forwarded to ProcessResource")
	dc, ok := mockInput.capturedCreds.(*credconfigv1.DirectCredentials)
	require.True(t, ok, "expected *credconfigv1.DirectCredentials, got %T", mockInput.capturedCreds)
	assert.Equal(t, "testuser", dc.Properties["username"])
	assert.Equal(t, "testpass", dc.Properties["password"])
}

// TestDefaultConstructor_attachOwnership_CallSiteGating proves the gates
// that now live at the by-reference call site (processResource): the hosting
// resource repository is resolved — and AddOwnership reached — only
// when the resource opts in via OwnershipPolicyAlways and a provider is
// configured. A non-opted-in resource must never touch the resource repository,
// which is what keeps non-OCI by-reference accesses out of OCI-specific
// resolution.
func TestDefaultConstructor_attachOwnership_CallSiteGating(t *testing.T) {
	const (
		component = "ocm.software/test-component"
		version   = "v1.0.0"
	)

	// byReferenceResource builds a by-reference (access, not input) resource with
	// the given ownership policy. An empty policy leaves the options block off.
	byReferenceResource := func(policy string) string {
		options := ""
		if policy != "" {
			options = fmt.Sprintf("\n        options:\n          ownershipPolicy: %s", policy)
		}
		return fmt.Sprintf(`
      - name: backend-image
        version: %s
        relation: local
        type: ociArtifact%s
        access:
          type: mock/v1
          mediaType: application/octet-stream
          reference: test-ref
`, version, options)
	}

	tests := []struct {
		name string
		// policy is the resource's ownershipPolicy ("" => no opt-in).
		policy string
		// provider builds opts.ResourceRepositoryProvider; nil means "not configured".
		provider   func(attacher *mockResourceRepository) ResourceRepositoryProvider
		wantErr    string
		wantAttach int
	}{
		{
			name:   "no opt-in never resolves the resource repository",
			policy: "",
			provider: func(a *mockResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{repo: a}
			},
			wantAttach: 0,
		},
		{
			name:       "opted in but no provider configured is a no-op",
			policy:     "Always",
			provider:   func(*mockResourceRepository) ResourceRepositoryProvider { return nil },
			wantAttach: 0,
		},
		{
			name:   "opted in resolves the repository and attaches",
			policy: "Always",
			provider: func(a *mockResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{repo: a}
			},
			wantAttach: 1,
		},
		{
			name:   "opted in surfaces a provider resolution failure",
			policy: "Always",
			provider: func(*mockResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProviderWithError{err: fmt.Errorf("boom")}
			},
			wantErr: "error getting resource repository for ownership",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attacher := &mockResourceRepository{}
			c := setupTestComponent(t, byReferenceResource(tt.policy))
			opts := Options{
				TargetRepositoryProvider:   &mockTargetRepositoryProvider{repo: newMockTargetRepository()},
				ResourceRepositoryProvider: tt.provider(attacher),
			}
			instance := NewDefaultConstructor(c, opts)

			err := instance.Construct(context.Background())

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAttach, attacher.attachCalls)
			if tt.wantAttach > 0 {
				assert.Equal(t, component, attacher.gotComponent)
				assert.Equal(t, version, attacher.gotVersion)
			}
		})
	}
}
