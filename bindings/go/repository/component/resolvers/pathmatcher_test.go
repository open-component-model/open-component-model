package providers

import (
	"context"
	"fmt"
	"sync"
	"testing"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
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
	repository.ComponentVersionRepository
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
	}

	// First call - should create repository
	repo1, err := provider.ResolveComponentVersionRepository(ctx, "example.com/component", "v1.0.0")
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForComponent failed: %v", err)
	}
	if repo1 == nil {
		t.Fatal("Expected repository, got nil")
	}

	// Second call with same component - should use cache
	repo2, err := provider.ResolveComponentVersionRepository(ctx, "example.com/component", "v2.0.0")
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
	}

	// Should succeed for valid spec
	repo, err := provider.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
	if err != nil {
		t.Fatalf("GetComponentVersionRepositoryForSpecification failed: %v", err)
	}
	if repo == nil {
		t.Fatal("Expected repository, got nil")
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
	}

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
