package constructor

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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

func TestAddColocatedResourceLocalBlob_AttachesOwnershipOptIn(t *testing.T) {
	const (
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)
	ownershipAwareWithAttachErr := newMockOwnershipAwareTargetRepository()
	ownershipAwareWithAttachErr.ownershipErr = fmt.Errorf("attach boom")

	tests := []struct {
		name            string
		policy          constructorruntime.OwnershipPolicy
		repo            TargetRepository
		wantCalls       int
		wantErr         bool
		wantErrContains []string
	}{
		{
			name:      "opted in (Always)",
			policy:    constructorruntime.OwnershipPolicyAlways,
			repo:      newMockOwnershipAwareTargetRepository(),
			wantCalls: 1,
		},
		{
			name:      "not opted in (Never)",
			policy:    constructorruntime.OwnershipPolicyNever,
			repo:      newMockOwnershipAwareTargetRepository(),
			wantCalls: 0,
		},
		{
			name:            "opted in but attach fails",
			policy:          constructorruntime.OwnershipPolicyAlways,
			repo:            ownershipAwareWithAttachErr,
			wantCalls:       1,
			wantErr:         true,
			wantErrContains: []string{"error attaching ownership", "attach boom"},
		},
		{
			name:            "opted in but repo cannot record ownership",
			policy:          constructorruntime.OwnershipPolicyAlways,
			repo:            newMockTargetRepository(),
			wantCalls:       0,
			wantErr:         true,
			wantErrContains: []string{"cannot record", "Always"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &constructorruntime.Resource{
				ElementMeta: constructorruntime.ElementMeta{ObjectMeta: constructorruntime.ObjectMeta{Name: "backend-image", Version: version}},
				Type:        "ociArtifact",
				Relation:    constructorruntime.LocalRelation,
				Options:     constructorruntime.ResourceOptions{OwnershipPolicy: tt.policy},
			}
			data := &mockBlob{mediaType: "application/octet-stream", data: []byte("payload")}

			out, err := addColocatedResourceLocalBlob(context.Background(), tt.repo, component, version, res, data)
			if tt.wantErr {
				require.Error(t, err)
				for _, s := range tt.wantErrContains {
					assert.ErrorContains(t, err, s)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, out)
			}

			if attacher, ok := tt.repo.(*mockOwnershipAwareTargetRepository); ok {
				assert.Equal(t, tt.wantCalls, attacher.ownershipCalls,
					"by-value add must attach ownership iff the runtime options opt in")
				if tt.wantCalls > 0 && !tt.wantErr {
					assert.Same(t, out, attacher.ownershipResource, "the uploaded resource must be forwarded to AddOwnership")
					assert.Nil(t, attacher.ownershipCreds, "AddOwnership on a component version repository must receive nil credentials")
				}
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

func TestDefaultConstructor_OwnershipAttachment(t *testing.T) {
	const (
		component = "ocm.software/test-component"
		version   = "v1.0.0"
	)

	tests := []struct {
		name string
		// policy is the resource's ownershipPolicy ("" => no opt-in).
		policy string
		// provider builds opts.ResourceRepositoryProvider; nil means "not configured".
		provider   func(attacher *mockOwnershipAwareResourceRepository) ResourceRepositoryProvider
		wantErr    string
		wantAttach int
	}{
		{
			name:   "no opt-in never resolves the resource repository",
			policy: "",
			provider: func(a *mockOwnershipAwareResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{repo: a}
			},
			wantAttach: 0,
		},
		{
			name:     "opted in but no provider configured is an error",
			policy:   "Always",
			provider: func(*mockOwnershipAwareResourceRepository) ResourceRepositoryProvider { return nil },
			wantErr:  "no resource repository provider is configured",
		},
		{
			name:   "opted in resolves the repository and attaches",
			policy: "Always",
			provider: func(a *mockOwnershipAwareResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{repo: a}
			},
			wantAttach: 1,
		},
		{
			name:   "opted in surfaces a provider resolution failure",
			policy: "Always",
			provider: func(*mockOwnershipAwareResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{err: fmt.Errorf("boom")}
			},
			wantErr: "error getting resource repository for ownership",
		},
		{
			name:   "opted in but repo cannot record ownership",
			policy: "Always",
			provider: func(*mockOwnershipAwareResourceRepository) ResourceRepositoryProvider {
				return &mockResourceRepositoryProvider{repo: &mockResourceRepository{}}
			},
			wantErr: "cannot record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attacher := newMockOwnershipAwareResourceRepository()
			resource := &constructorruntime.Resource{
				ElementMeta: constructorruntime.ElementMeta{
					ObjectMeta: constructorruntime.ObjectMeta{
						Name:    "backend-image",
						Version: version,
					},
				},
				Type:     "ociArtifact",
				Relation: constructorruntime.LocalRelation,
				AccessOrInput: constructorruntime.AccessOrInput{
					Access: &runtime.Raw{
						Type: runtime.NewVersionedType("mock", "v1"),
						Data: []byte(`{"type":"mock/v1","mediaType":"application/octet-stream","reference":"test-ref"}`),
					},
				},
				Options: constructorruntime.ResourceOptions{OwnershipPolicy: constructorruntime.OwnershipPolicy(tt.policy)},
			}
			opts := Options{
				ResourceRepositoryProvider: tt.provider(attacher),
			}
			c := NewDefaultConstructor(&constructorruntime.ComponentConstructor{}, opts).(*DefaultConstructor)

			_, err := c.processResource(context.Background(), newMockTargetRepository(), resource, component, version)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAttach, attacher.ownershipCalls)
			if tt.wantAttach > 0 {
				assert.Equal(t, component, attacher.ownershipComponent)
				assert.Equal(t, version, attacher.ownershipVersion)
			}
		})
	}
}
