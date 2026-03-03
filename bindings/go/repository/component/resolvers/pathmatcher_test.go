package resolvers

import (
	"context"
	"fmt"
	"sync"
	"testing"

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
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

	provider := &pathMatcherResolver{
		repoProvider: mockProvider,
		graph:        nil,
		specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
	}

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

	provider := &pathMatcherResolver{
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

	provider := &pathMatcherResolver{
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

// TestGetRepositorySpecForComponent tests both path matcher and fallback resolvers
func TestGetRepositorySpecForComponent(t *testing.T) {
	ctx := context.Background()

	repoSpecA := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","name":"repo-a"}`),
	}

	repoSpecB := &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(`{"type":"test-repo","name":"repo-b"}`),
	}

	tests := []struct {
		name      string
		component string
		version   string
		wantSpec  runtime.Typed
		wantErr   bool
	}{
		{
			name:      "component matches first pattern/repo",
			component: "example.com/a/component",
			version:   "v1.0.0",
			wantSpec:  repoSpecA,
			wantErr:   false,
		},
		{
			name:      "component matches second pattern/repo",
			component: "example.com/b/component",
			version:   "v1.0.0",
			wantSpec:  repoSpecB,
			wantErr:   false,
		},
		{
			name:      "component matches no pattern/repo",
			component: "example.com/c/component",
			version:   "v1.0.0",
			wantSpec:  nil,
			wantErr:   true,
		},
	}

	runTests := func(t *testing.T, resolver ComponentVersionRepositoryResolver) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				spec, err := resolver.GetRepositorySpecificationForComponent(ctx, tt.component, tt.version)
				if (err != nil) != tt.wantErr {
					t.Errorf("GetRepositorySpecificationForComponent() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if err != nil {
					return
				}

				wantRaw := tt.wantSpec.(*runtime.Raw)
				gotRaw, ok := spec.(*runtime.Raw)
				if !ok {
					t.Errorf("GetRepositorySpecificationForComponent() returned type %T, want *runtime.Raw", spec)
					return
				}
				if string(gotRaw.Data) != string(wantRaw.Data) {
					t.Errorf("GetRepositorySpecificationForComponent() = %s, want %s", string(gotRaw.Data), string(wantRaw.Data))
				}
			})
		}
	}

	t.Run("PathMatcherResolver", func(t *testing.T) {
		mockProvider := &mockRepoProvider{}

		resolvers := []*resolverspec.Resolver{
			{
				Repository:           repoSpecA,
				ComponentNamePattern: "example.com/a/*",
			},
			{
				Repository:           repoSpecB,
				ComponentNamePattern: "example.com/b/*",
			},
		}

		provider := &pathMatcherResolver{
			repoProvider: mockProvider,
			graph:        nil,
			specProvider: pathmatcher.NewSpecProvider(ctx, resolvers),
			repoCache:    make(map[string]repository.ComponentVersionRepository),
		}

		runTests(t, provider)
	})

	t.Run("FallbackResolver", func(t *testing.T) {
		mockProvider := &fallbackMockRepoProvider{
			repoSpecs: map[string]runtime.Typed{
				"repo-a": repoSpecA,
				"repo-b": repoSpecB,
			},
			repoComponents: map[string]map[string][]string{
				"repo-a": {"example.com/a/component": {"v1.0.0"}},
				"repo-b": {"example.com/b/component": {"v1.0.0"}},
			},
		}

		//nolint:staticcheck // testing deprecated fallback resolvers
		fallbackResolvers := []*resolverruntime.Resolver{
			{
				Repository: repoSpecA,
				Prefix:     "example.com/a",
				Priority:   1,
			},
			{
				Repository: repoSpecB,
				Prefix:     "example.com/b",
				Priority:   1,
			},
		}

		resolver, err := New(ctx, Options{
			RepoProvider:      mockProvider,
			FallbackResolvers: fallbackResolvers,
		}, nil)
		if err != nil {
			t.Fatalf("failed to create fallback resolver: %v", err)
		}

		runTests(t, resolver)
	})
}

// fallbackMockRepoProvider implements repository.ComponentVersionRepositoryProvider for fallback resolver testing
type fallbackMockRepoProvider struct {
	repoSpecs      map[string]runtime.Typed       // name -> spec
	repoComponents map[string]map[string][]string // name -> component -> versions
}

func (m *fallbackMockRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("not implemented for test")
}

func (m *fallbackMockRepoProvider) GetComponentVersionRepository(_ context.Context, spec runtime.Typed, _ map[string]string) (repository.ComponentVersionRepository, error) {
	raw, ok := spec.(*runtime.Raw)
	if !ok {
		return nil, fmt.Errorf("unexpected spec type: %T", spec)
	}

	// Extract repo name from spec data
	for name, repoSpec := range m.repoSpecs {
		if repoRaw, ok := repoSpec.(*runtime.Raw); ok {
			if string(repoRaw.Data) == string(raw.Data) {
				return &fallbackMockRepo{
					name:       name,
					components: m.repoComponents[name],
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown repository spec")
}

func (m *fallbackMockRepoProvider) GetJSONSchemaForRepositorySpecification(_ runtime.Type) ([]byte, error) {
	return nil, fmt.Errorf("not implemented for test")
}

// fallbackMockRepo implements repository.ComponentVersionRepository for fallback resolver testing
type fallbackMockRepo struct {
	name       string
	components map[string][]string // component -> versions
	repository.ComponentVersionRepository
}

func (m *fallbackMockRepo) GetComponentVersion(_ context.Context, component, version string) (*descriptor.Descriptor, error) {
	if versions, ok := m.components[component]; ok {
		for _, v := range versions {
			if v == version {
				return &descriptor.Descriptor{
					Component: descriptor.Component{
						ComponentMeta: descriptor.ComponentMeta{
							ObjectMeta: descriptor.ObjectMeta{
								Name:    component,
								Version: version,
							},
						},
					},
				}, nil
			}
		}
	}
	return nil, repository.ErrNotFound
}
