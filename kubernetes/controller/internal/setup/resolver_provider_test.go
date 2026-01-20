package setup_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// mockResolverProvider implements repository.ComponentVersionRepositoryProvider for testing
type mockResolverProvider struct{}

func (m *mockResolverProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func (m *mockResolverProvider) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return nil
}

var _ repository.ComponentVersionRepositoryProvider = (*mockResolverProvider)(nil)

func (m *mockResolverProvider) GetComponentVersionRepository(ctx context.Context, spec runtime.Typed, attrs map[string]string) (repository.ComponentVersionRepository, error) {
	return nil, nil
}

func (m *mockResolverProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

// mockRepoSpec creates a mock repository specification for testing
func mockRepoSpec() runtime.Typed {
	return &ociv1.Repository{
		BaseUrl: "mock.io/test",
	}
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
					},
					{
						"componentNamePattern": "ocm.software/*",
						"repository": {
							"type": "oci/v1",
							"baseUrl": "registry.io"
						}
					}
				]
			}
		]
	}`
	return createConfigFromJSON(configJSON)
}

func TestGetResolversV1Alpha1(t *testing.T) {
	t.Run("ExtractsResolversFromConfig", func(t *testing.T) {
		config := createPathMatcherConfig()

		resolvers, err := setup.GetResolversV1Alpha1(config)

		require.NoError(t, err)
		require.NotNil(t, resolvers)
		require.Len(t, resolvers, 2)
		assert.Equal(t, "my-comp-*", resolvers[0].ComponentNamePattern)
		assert.Equal(t, "ocm.software/*", resolvers[1].ComponentNamePattern)
	})

	t.Run("ReturnsNilForEmptyConfig", func(t *testing.T) {
		config := &genericv1.Config{}

		resolvers, err := setup.GetResolversV1Alpha1(config)

		require.NoError(t, err)
		assert.Nil(t, resolvers)
	})

	t.Run("ReturnsNilForNilConfig", func(t *testing.T) {
		resolvers, err := setup.GetResolversV1Alpha1(nil)

		require.NoError(t, err)
		assert.Nil(t, resolvers)
	})

	t.Run("ReturnsNilForConfigWithoutResolvers", func(t *testing.T) {
		configJSON := `{
			"type": "generic.config.ocm.software/v1",
			"configurations": [
				{
					"type": "credentials.config.ocm.software",
					"repositories": []
				}
			]
		}`
		config := createConfigFromJSON(configJSON)

		resolvers, err := setup.GetResolversV1Alpha1(config)

		require.NoError(t, err)
		assert.Nil(t, resolvers)
	})
}

func TestNewResolverProvider(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	provider := &mockResolverProvider{}

	t.Run("CreatesProviderWithResolvers", func(t *testing.T) {
		resolvers := []*resolverspec.Resolver{
			{
				Repository: &runtime.Raw{
					Type: runtime.Type{Name: "oci", Version: "v1"},
					Data: []byte(`{"type":"oci/v1","baseUrl":"ghcr.io"}`),
				},
				ComponentNamePattern: "ocm.software/*",
			},
		}

		opts := setup.ResolverProviderOptions{
			Registry:  provider,
			Logger:    &logger,
			Resolvers: resolvers,
		}

		result, err := setup.NewResolverProvider(ctx, opts)

		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("ErrorWhenNoRegistry", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Resolvers: []*resolverspec.Resolver{{}},
		}

		result, err := setup.NewResolverProvider(ctx, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "component version registry is required")
		assert.Nil(t, result)
	})

	t.Run("ErrorWhenNoResolvers", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Registry:  provider,
			Resolvers: []*resolverspec.Resolver{},
		}

		result, err := setup.NewResolverProvider(ctx, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one resolver must be provided")
		assert.Nil(t, result)
	})
}

func TestNewSimpleResolverProvider(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	provider := &mockResolverProvider{}

	t.Run("CreatesProviderWithWildcardMatcher", func(t *testing.T) {
		repo := mockRepoSpec()
		opts := setup.ResolverProviderOptions{
			Registry: provider,
			Logger:   &logger,
		}

		result, err := setup.NewSimpleResolverProvider(ctx, opts, repo)

		require.NoError(t, err)
		require.NotNil(t, result)

		// Should resolve any component (wildcard matches all)
		compRepo, err := result.GetComponentVersionRepositoryForComponent(ctx, "ocm.software/component", "1.0.0")
		require.NoError(t, err)
		assert.Nil(t, compRepo, "mock provider returns nil repository")

		compRepo, err = result.GetComponentVersionRepositoryForComponent(ctx, "ghcr.io/org/repo", "2.0.0")
		require.NoError(t, err)
		assert.Nil(t, compRepo, "mock provider returns nil repository")
	})

	t.Run("ErrorWhenNoRegistry", func(t *testing.T) {
		repo := mockRepoSpec()
		opts := setup.ResolverProviderOptions{}

		result, err := setup.NewSimpleResolverProvider(ctx, opts, repo)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "component version registry is required")
		assert.Nil(t, result)
	})

	t.Run("ErrorWhenNoRepository", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Registry: provider,
		}

		result, err := setup.NewSimpleResolverProvider(ctx, opts, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "repository is required")
		assert.Nil(t, result)
	})
}

func TestNewResolverProviderWithRepository(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	provider := &mockResolverProvider{}
	repo := mockRepoSpec()

	t.Run("CreatesProviderWithComponentPatterns", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Registry: provider,
			Logger:   &logger,
		}
		patterns := []string{"ocm.software/mycomponent", "github.com/*"}

		result, err := setup.NewResolverProviderWithRepository(ctx, opts, repo, patterns)

		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("CreatesProviderWithConfigResolvers", func(t *testing.T) {
		configResolvers := []*resolverspec.Resolver{
			{
				Repository: &runtime.Raw{
					Type: runtime.Type{Name: "oci", Version: "v1"},
					Data: []byte(`{"type":"oci/v1","baseUrl":"config.io"}`),
				},
				ComponentNamePattern: "config/*",
			},
		}
		opts := setup.ResolverProviderOptions{
			Registry:  provider,
			Logger:    &logger,
			Resolvers: configResolvers,
		}
		patterns := []string{"ocm.software/priority"}

		result, err := setup.NewResolverProviderWithRepository(ctx, opts, repo, patterns)

		require.NoError(t, err)
		require.NotNil(t, result)
		// Priority should be: patterns -> config -> wildcard
	})

	t.Run("CreatesProviderWithoutPatterns", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Registry: provider,
			Logger:   &logger,
		}

		result, err := setup.NewResolverProviderWithRepository(ctx, opts, repo, nil)

		require.NoError(t, err)
		require.NotNil(t, result)
		// Should create with wildcard at beginning and end
	})

	t.Run("ErrorWhenNoRegistry", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{}

		result, err := setup.NewResolverProviderWithRepository(ctx, opts, repo, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "component version registry is required")
		assert.Nil(t, result)
	})

	t.Run("ErrorWhenNoRepository", func(t *testing.T) {
		opts := setup.ResolverProviderOptions{
			Registry: provider,
		}

		result, err := setup.NewResolverProviderWithRepository(ctx, opts, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "base repository is required")
		assert.Nil(t, result)
	})
}

func TestResolverProviderIntegration(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	t.Run("ExtractAndUseResolversFromConfig", func(t *testing.T) {
		config := createPathMatcherConfig()
		provider := &mockResolverProvider{}

		// Extract resolvers from config
		resolvers, err := setup.GetResolversV1Alpha1(config)
		require.NoError(t, err)
		require.Len(t, resolvers, 2)

		// Create resolver provider
		opts := setup.ResolverProviderOptions{
			Registry:  provider,
			Logger:    &logger,
			Resolvers: resolvers,
		}
		resolverProvider, err := setup.NewResolverProvider(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, resolverProvider)

		// Test resolution (should not error with mock)
		repo, err := resolverProvider.GetComponentVersionRepositoryForComponent(ctx, "my-comp-test", "1.0.0")
		require.NoError(t, err)
		assert.Nil(t, repo, "mock provider returns nil")
	})
}
