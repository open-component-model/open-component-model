package ocm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// mockRepositoryProvider implements repository.ComponentVersionRepositoryProvider for testing
type mockRepositoryProvider struct{}

func (m *mockRepositoryProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func (m *mockRepositoryProvider) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return nil
}

var _ repository.ComponentVersionRepositoryProvider = (*mockRepositoryProvider)(nil)

func (m *mockRepositoryProvider) GetComponentVersionRepository(ctx context.Context, spec runtime.Typed, attrs map[string]string) (repository.ComponentVersionRepository, error) {
	return nil, nil
}

func (m *mockRepositoryProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

// mockRepository creates a mock repository specification for testing
func mockRepository() runtime.Typed {
	repoJSON := `{"type":"oci/v1","baseUrl":"mock.io"}`
	return &runtime.Raw{
		Type: runtime.Type{
			Name:    "oci",
			Version: "v1",
		},
		Data: []byte(repoJSON),
	}
}

// isPathMatcherProvider checks if v is a path matcher provider
func isPathMatcherProvider(v interface{}) bool {
	return strings.Contains(fmt.Sprintf("%T", v), "pathMatcher") || strings.Contains(fmt.Sprintf("%T", v), "resolverProvider")
}

// isFallbackProvider checks if v is a fallback provider
func isFallbackProvider(v interface{}) bool {
	return strings.Contains(fmt.Sprintf("%T", v), "fallback")
}

// createConfigFromJSON creates a genericv1.Config from JSON string
func createConfigFromJSON(configJSON string) *genericv1.Config {
	config := &genericv1.Config{}
	if err := json.Unmarshal([]byte(configJSON), config); err != nil {
		panic(err)
	}
	return config
}

// createPathMatcherConfig creates a config with path matcher resolvers
func createPathMatcherConfig() *genericv1.Config {
	configJSON := `{
		"type": "generic.config.ocm.software/v1",
		"configurations": [
			{
				"type": "resolvers.config.ocm.software",
				"resolvers": [
					{
						"componentNamePattern": "my-comp-*",
						"repository": {
							"type": "oci/v1",
							"baseUrl": "ghcr.io"
						}
					}
				]
			}
		]
	}`
	return createConfigFromJSON(configJSON)
}

// createFallbackResolversConfig creates a config with deprecated fallback resolvers
func createFallbackResolversConfig() *genericv1.Config {
	configJSON := `{
		"type": "generic.config.ocm.software/v1",
		"configurations": [
			{
				"type": "ocm.config.ocm.software",
				"resolvers": [
					{
						"repository": {
							"type": "oci/v1",
							"baseUrl": "ghcr.io",
							"priority": 100
						}
					}
				]
			}
		]
	}`
	return createConfigFromJSON(configJSON)
}

func TestNewComponentRepositoryProvider(t *testing.T) {
	t.Run("SimplePathMatcher", func(t *testing.T) {
		// Tests for createSimplePathMatcherProvider - repository only with wildcard "*"
		t.Run("WithRepositoryOnly", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithRepository(repo),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Should create path matcher provider with wildcard "*"
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)

			// Should resolve any component (wildcard matches all)
			compRepo, err := result.GetComponentVersionRepositoryForComponent(ctx, "ocm.software/component", "1.0.0")
			require.NoError(t, err, "should resolve any component with wildcard pattern")
			require.Nil(t, compRepo, "mock provider returns nil repository")
			compRepo, err = result.GetComponentVersionRepositoryForComponent(ctx, "ghcr.io/org/repo", "1.0.0")
			require.NoError(t, err, "should resolve any component with wildcard pattern")
			require.Nil(t, compRepo, "mock provider returns nil repository")
			compRepo, err = result.GetComponentVersionRepositoryForComponent(ctx, "example.com/test", "1.0.0")
			require.NoError(t, err, "should resolve any component with wildcard pattern")
			require.Nil(t, compRepo, "mock provider returns nil repository")
		})

		t.Run("WithRepositoryAndComponentPattern", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithRepository(repo),
				WithComponentPatterns([]string{"ocm.software/root"}),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Component pattern is ignored without config - should still create simple resolver
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("ReturnsNilWhenNeitherRepositoryNorConfig", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}

			result, err := NewComponentRepositoryProvider(ctx, provider, nil)

			require.NoError(t, err)
			require.Nil(t, result)
		})
	})

	t.Run("PathMatcher", func(t *testing.T) {
		// Tests for createPathMatcherProvider - with config patterns and component patterns

		t.Run("WithPathMatchersOnly", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			config := createPathMatcherConfig()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithRepositoryAndPathMatcherConfig", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			config := createPathMatcherConfig()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithRepository(repo),
				WithComponentPatterns([]string{"ocm.software/root"}),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Should merge: component pattern (highest), config resolvers (middle), wildcard (lowest)
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithComponentRefOnly", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			ref := &compref.Ref{
				Repository: repo,
				Component:  "ocm.software/mycomponent",
			}

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithComponentRef(ref),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Should create simple path matcher provider
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithComponentRefAndPathMatcherConfig", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			config := createPathMatcherConfig()
			ref := &compref.Ref{
				Repository: repo,
				Component:  "ocm.software/component-a",
			}

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithComponentRef(ref),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Should create path matcher provider
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithMultipleComponentPatterns", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			config := createPathMatcherConfig()

			patterns := []string{"ocm.software/*", "example.com/*"}
			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithRepository(repo),
				WithComponentPatterns(patterns),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Multiple patterns should all be included
			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithAllThreeOptions_PriorityTesting", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			config := createPathMatcherConfig() // Has pattern "my-comp-*"

			// Component patterns should have highest priority
			componentPatterns := []string{"priority.test/*"}
			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithRepository(repo),
				WithComponentPatterns(componentPatterns),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})

		t.Run("WithConfigRepositoryAndComponentPatterns", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			config := createPathMatcherConfig()

			// Component pattern that overlaps with config pattern
			componentPatterns := []string{"my-comp-priority-*"}
			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithRepository(repo),
				WithComponentPatterns(componentPatterns),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			require.True(t, isPathMatcherProvider(result), "expected pathMatcherProvider, got %T", result)
		})
	})

	t.Run("FallbackResolver", func(t *testing.T) {
		// Tests for createFallbackProvider - deprecated fallback resolvers

		t.Run("WithFallbackResolversOnly", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			//nolint:staticcheck // testing deprecated feature
			config := createFallbackResolversConfig()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			require.True(t, isFallbackProvider(result), "expected fallbackProvider, got %T", result)

		})

		t.Run("OptionsShouldBeIgnoredWhenFallbackResolversConfigIsUsed", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			repo := mockRepository()
			//nolint:staticcheck // testing deprecated feature
			config := createFallbackResolversConfig()

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithRepository(repo),
				WithComponentPatterns([]string{"ocm.software/component"}),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			require.True(t, isFallbackProvider(result), "expected fallbackProvider, got %T", result)
		})
	})

	t.Run("ShouldReturnError", func(t *testing.T) {
		t.Run("WhenBothResolverTypesConfigured", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			//nolint:staticcheck // testing error case with mixed resolver types
			config := createConfigFromJSON(
				`{
		"type": "generic.config.ocm.software/v1",
		"configurations": [
			{
				"type": "resolvers.config.ocm.software",
				"resolvers": [
					{
						"componentNamePattern": "my-comp-*",
						"repository": {
							"type": "oci/v1",
							"baseUrl": "ghcr.io"
						}
					}
				]
			},
			{
				"type": "ocm.config.ocm.software",
				"resolvers": [
					{
						"repository": {
							"type": "oci/v1",
							"baseUrl": "ghcr.io",
							"priority": 100
						}
					}
				]
			}
		]
	}`)

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
			)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, "both path matcher and fallback resolvers are configured, only one type is allowed", err.Error())
		})

		t.Run("WithPathMatchersWhenComponentDoesNotMatch", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			config := createPathMatcherConfig() // Only has "my-comp-*" pattern

			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Component matching the pattern should work
			_, err = result.GetComponentVersionRepositoryForComponent(ctx, "my-comp-theta", "1.0.0")
			require.NoError(t, err, "should resolve component matching pattern")

			// Component not matching the pattern should fail
			_, err = result.GetComponentVersionRepositoryForComponent(ctx, "ocm.software/component", "1.0.0")
			require.Error(t, err, "should fail to resolve component not matching any pattern")

			_, err = result.GetComponentVersionRepositoryForComponent(ctx, "example.com/other", "1.0.0")
			require.Error(t, err, "should fail to resolve component not matching any pattern")
		})

		t.Run("WithComponentPatternsOnly_UnknownComponents", func(t *testing.T) {
			ctx := context.Background()
			provider := &mockRepositoryProvider{}
			config := createPathMatcherConfig() // Has pattern "my-comp-*"

			// Test with ONLY component patterns and config (no repository wildcard fallback)
			patterns := []string{"ocm.software/*", "example.com/*"}
			result, err := NewComponentRepositoryProvider(ctx, provider, nil,
				WithConfig(config),
				WithComponentPatterns(patterns),
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Should NOT resolve components that don't match any pattern (no wildcard fallback)
			compRepo, err := result.GetComponentVersionRepositoryForComponent(ctx, "unknown.domain/component", "1.0.0")
			require.Error(t, err, "should fail to resolve component not matching any pattern")
			require.Nil(t, compRepo, "should return nil repository on error")
		})
	})
}
