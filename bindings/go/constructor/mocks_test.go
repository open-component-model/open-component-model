package constructor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockTargetRepository is a bare [TargetRepository] without ownership support.
// Compose it with [mockOwnershipAttacher] via [mockOwnershipAwareTargetRepository]
// to opt into [repository.OwnershipAwareRepository].
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

func (m *mockTargetRepository) AddLocalResource(ctx context.Context, component, version string, resource *descriptor.Resource, data blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedLocalResources = append(m.addedLocalResources, resource)
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

// mockOwnershipAttacher records calls to [repository.OwnershipAwareRepository.AddOwnership].
type mockOwnershipAttacher struct {
	mu                 sync.Mutex
	ownershipCalls     int
	ownershipComponent string
	ownershipVersion   string
	ownershipResource  *descriptor.Resource
	ownershipCreds     runtime.Typed
	ownershipErr       error
}

func (o *mockOwnershipAttacher) AddOwnership(ctx context.Context, component, version string, resource *descriptor.Resource, credentials runtime.Typed) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ownershipCalls++
	o.ownershipComponent = component
	o.ownershipVersion = version
	o.ownershipResource = resource
	o.ownershipCreds = credentials
	return o.ownershipErr
}

type mockOwnershipAwareTargetRepository struct {
	*mockTargetRepository
	*mockOwnershipAttacher
}

func newMockOwnershipAwareTargetRepository() *mockOwnershipAwareTargetRepository {
	return &mockOwnershipAwareTargetRepository{
		mockTargetRepository:  newMockTargetRepository(),
		mockOwnershipAttacher: &mockOwnershipAttacher{},
	}
}

type mockTargetRepositoryProvider struct {
	repo TargetRepository
}

func (m *mockTargetRepositoryProvider) GetTargetRepository(ctx context.Context, component *constructorruntime.Component) (TargetRepository, error) {
	return m.repo, nil
}

type mockResourceRepository struct {
	downloadData blob.ReadOnlyBlob
	fail         bool

	identityErr error
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

type mockOwnershipAwareResourceRepository struct {
	*mockResourceRepository
	*mockOwnershipAttacher
}

func newMockOwnershipAwareResourceRepository() *mockOwnershipAwareResourceRepository {
	return &mockOwnershipAwareResourceRepository{
		mockResourceRepository: &mockResourceRepository{},
		mockOwnershipAttacher:  &mockOwnershipAttacher{},
	}
}

type mockResourceRepositoryProvider struct {
	repo ResourceRepository
	err  error
}

func (m *mockResourceRepositoryProvider) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (ResourceRepository, error) {
	return m.repo, m.err
}

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

type mockInputMethodProvider struct {
	methods map[runtime.Type]ResourceInputMethod
}

func (m *mockInputMethodProvider) GetResourceInputMethod(ctx context.Context, resource *constructorruntime.Resource) (ResourceInputMethod, error) {
	if method, ok := m.methods[resource.Input.GetType()]; ok {
		return method, nil
	}
	return nil, fmt.Errorf("no input method resolvable for input specification of type %s", resource.Input.GetType())
}

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

func (m *mockSourceInputMethod) ProcessSource(ctx context.Context, source *constructorruntime.Source, creds runtime.Typed) (*SourceInputMethodResult, error) {
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

type mockSourceInputMethodProvider struct {
	methods map[runtime.Type]SourceInputMethod
}

func (m *mockSourceInputMethodProvider) GetSourceInputMethod(ctx context.Context, source *constructorruntime.Source) (SourceInputMethod, error) {
	if method, ok := m.methods[source.Input.GetType()]; ok {
		return method, nil
	}
	return nil, fmt.Errorf("no input method resolvable for input specification of type %s", source.Input.GetType())
}

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

type mockDigestProcessorProvider struct {
	processor ResourceDigestProcessor
}

func (m *mockDigestProcessorProvider) GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (ResourceDigestProcessor, error) {
	return m.processor, nil
}

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

type mockInputType struct {
	Type runtime.Type
}

func (m *mockInputType) GetType() runtime.Type {
	return m.Type
}

func (m *mockInputType) SetType(typ runtime.Type) {
	m.Type = typ
}

func (m *mockInputType) DeepCopyTyped() runtime.Typed {
	return &mockInputType{
		Type: m.Type,
	}
}

type mockCallbackTracker struct {
	startComponentCalled bool
	endComponentCalled   bool
	startResourceCalled  bool
	endResourceCalled    bool
	startSourceCalled    bool
	endSourceCalled      bool
	component            *constructorruntime.Component
	resource             *constructorruntime.Resource
	source               *constructorruntime.Source
	descriptor           *descriptor.Descriptor
	err                  error
}

func (m *mockCallbackTracker) reset() {
	m.startComponentCalled = false
	m.endComponentCalled = false
	m.startResourceCalled = false
	m.endResourceCalled = false
	m.startSourceCalled = false
	m.endSourceCalled = false
	m.component = nil
	m.resource = nil
	m.source = nil
	m.descriptor = nil
	m.err = nil
}
