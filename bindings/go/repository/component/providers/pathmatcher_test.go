package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"ocm.software/open-component-model/bindings/go/blob"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepoProvider implements repository.ComponentVersionRepositoryProvider for testing
type mockRepoProvider struct {
	callCount int
	mu        sync.Mutex
}

func (m *mockRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepoProvider) GetComponentVersionRepository(ctx context.Context, spec runtime.Typed, creds map[string]string) (repository.ComponentVersionRepository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	return &mockRepo{spec: spec}, nil
}

func (m *mockRepoProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, fmt.Errorf("not implemented for test")
}

// mockRepo implements repository.ComponentVersionRepository for testing
type mockRepo struct {
	spec runtime.Typed
}

func (m *mockRepo) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return fmt.Errorf("not implemented for test")
}

func (m *mockRepo) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepo) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepo) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepo) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepo) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *mockRepo) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, fmt.Errorf("not implemented for test")
}

// TestPathMatcherProvider_Caching verifies that repositories are cached
func TestPathMatcherProvider_Caching(t *testing.T) {
	ctx := context.Background()

	// Create a test repository spec
	repoSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","url":"https://example.com/repo"}`),
	}

	mockProvider := &mockRepoProvider{}

	// Create resolvers
	resolvers := []*resolverspec.Resolver{
		{
			Repository:           repoSpec,
			ComponentNamePattern: "example.com/*",
		},
	}

	provider := &pathMatcherProvider{
		repoProvider: mockProvider,
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
		validSpecs:   make(map[string]struct{}),
	}

	// Precompute valid specs
	data, _ := json.Marshal(repoSpec)
	data, _ = jsoncanonicalizer.Transform(data)
	provider.validSpecs[string(data)] = struct{}{}

	// First call - should create repository
	repo1, err := provider.GetComponentVersionRepositoryForComponent(ctx, "example.com/component", "v1.0.0")
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForComponent failed: %v", err)
	}
	if repo1 == nil {
		t.Fatal("Expected repository, got nil")
	}

	// Second call with same component - should use cache
	repo2, err := provider.GetComponentVersionRepositoryForComponent(ctx, "example.com/component", "v2.0.0")
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForComponent failed: %v", err)
	}
	if repo2 == nil {
		t.Fatal("Expected repository, got nil")
	}

	// Verify the provider was only called once (cache hit on second call)
	mockProvider.mu.Lock()
	callCount := mockProvider.callCount
	mockProvider.mu.Unlock()

	if callCount != 1 {
		t.Errorf("Expected 1 call to GetComponentVersionRepository, got %d", callCount)
	}

	// Verify both calls returned the same cached instance
	if repo1 != repo2 {
		t.Error("Expected cached repository to be returned")
	}
}

// TestPathMatcherProvider_GetRepositoryForSpecification_Valid verifies valid spec lookup
func TestPathMatcherProvider_GetRepositoryForSpecification_Valid(t *testing.T) {
	ctx := context.Background()

	repoSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","url":"https://example.com/repo"}`),
	}

	mockProvider := &mockRepoProvider{}

	resolvers := []*resolverspec.Resolver{
		{
			Repository:           repoSpec,
			ComponentNamePattern: "*",
		},
	}

	provider := &pathMatcherProvider{
		repoProvider: mockProvider,
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
		validSpecs:   make(map[string]struct{}),
	}

	// Precompute valid specs
	data, _ := json.Marshal(repoSpec)
	data, _ = jsoncanonicalizer.Transform(data)
	provider.validSpecs[string(data)] = struct{}{}

	// Should succeed for valid spec
	repo, err := provider.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForSpecification failed: %v", err)
	}
	if repo == nil {
		t.Fatal("Expected repository, got nil")
	}
}

// TestPathMatcherProvider_GetRepositoryForSpecification_Invalid verifies invalid spec rejection
func TestPathMatcherProvider_GetRepositoryForSpecification_Invalid(t *testing.T) {
	ctx := context.Background()

	repoSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","url":"https://example.com/repo"}`),
	}

	invalidSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("invalid-repo"),
		Data: []byte(`{"type":"invalid-repo","url":"https://invalid.com/repo"}`),
	}

	mockProvider := &mockRepoProvider{}

	resolvers := []*resolverspec.Resolver{
		{
			Repository:           repoSpec,
			ComponentNamePattern: "*",
		},
	}

	provider := &pathMatcherProvider{
		repoProvider: mockProvider,
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
		validSpecs:   make(map[string]struct{}),
	}

	// Precompute valid specs (only repoSpec, not invalidSpec)
	data, _ := json.Marshal(repoSpec)
	data, _ = jsoncanonicalizer.Transform(data)
	provider.validSpecs[string(data)] = struct{}{}

	// Should fail for invalid spec
	repo, err := provider.GetComponentVersionRepositoryForSpecification(ctx, invalidSpec)
	if err == nil {
		t.Fatal("Expected error for invalid spec, got nil")
	}
	if repo != nil {
		t.Fatal("Expected nil repository for invalid spec")
	}
}

// TestPathMatcherProvider_GetRepositoryForSpecification_Caching verifies caching works for GetComponentVersionRepositoryForSpecification
func TestPathMatcherProvider_GetRepositoryForSpecification_Caching(t *testing.T) {
	ctx := context.Background()

	repoSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","url":"https://example.com/repo"}`),
	}

	mockProvider := &mockRepoProvider{}

	resolvers := []*resolverspec.Resolver{
		{
			Repository:           repoSpec,
			ComponentNamePattern: "*",
		},
	}

	provider := &pathMatcherProvider{
		repoProvider: mockProvider,
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
		validSpecs:   make(map[string]struct{}),
	}

	// Precompute valid specs
	data, _ := json.Marshal(repoSpec)
	data, _ = jsoncanonicalizer.Transform(data)
	provider.validSpecs[string(data)] = struct{}{}

	// First call
	repo1, err := provider.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForSpecification failed: %v", err)
	}

	// Second call
	repo2, err := provider.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForSpecification failed: %v", err)
	}

	// Verify only one call to provider (second was cached)
	mockProvider.mu.Lock()
	callCount := mockProvider.callCount
	mockProvider.mu.Unlock()

	if callCount != 1 {
		t.Errorf("Expected 1 call to GetComponentVersionRepository, got %d", callCount)
	}

	// Verify same instance returned
	if repo1 != repo2 {
		t.Error("Expected cached repository to be returned")
	}
}

// TestPathMatcherProvider_GetRepositorySpecForComponent verifies spec resolution
func TestPathMatcherProvider_GetRepositorySpecForComponent(t *testing.T) {
	ctx := context.Background()

	repoSpec := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","url":"https://example.com/repo"}`),
	}

	resolvers := []*resolverspec.Resolver{
		{
			Repository:           repoSpec,
			ComponentNamePattern: "example.com/*",
		},
	}

	provider := &pathMatcherProvider{
		repoProvider: &mockRepoProvider{},
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
		validSpecs:   make(map[string]struct{}),
	}

	// Should resolve spec for matching component
	spec, err := provider.GetRepositorySpecForComponent(ctx, "example.com/component", "v1.0.0")
	if err != nil {
		t.Fatalf("GetRepositorySpecForComponent failed: %v", err)
	}
	if spec == nil {
		t.Fatal("Expected spec, got nil")
	}

	// Verify spec type matches
	if spec.GetType().String() != "test-repo" {
		t.Errorf("Expected spec type 'test-repo', got '%s'", spec.GetType().String())
	}
}
