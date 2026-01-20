package resolution_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/hashicorp/golang-lru/v2/expirable"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

func TestResolverIntegration(t *testing.T) {
	ctx := context.Background()
	logger := &logr.Logger{}

	// Create a config with path matcher resolvers
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-config-resolvers",
			Namespace: "default",
		},
		Data: map[string]string{
			".ocmconfig": `{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "credentials.config.ocm.software",
						"repositories": []
					},
					{
						"type": "resolvers.config.ocm.software",
						"resolvers": [
							{
								"componentNamePattern": "github.com/test/*",
								"repository": {
									"type": "oci/v1",
									"baseUrl": "ghcr.io/test-org"
								}
							}
						]
					}
				]
			}`,
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configMap).
		Build()

	pm := manager.NewPluginManager(ctx)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
	})

	// Register a test plugin
	registerTestOCIPlugin(t, pm, "github.com/test/mycomponent", "v1.0.0")

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		Logger: logger,
		Client: k8sClient,
		Cache:  cache,
	})

	resolver := resolution.NewResolver(k8sClient, logger, wp, pm)

	t.Run("uses path matcher resolvers from config", func(t *testing.T) {
		opts := &resolution.RepositoryOptions{
			RepositorySpec: nil, // No repository spec - rely on resolvers
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config-resolvers",
					},
				},
			},
			Namespace: "default",
		}

		cachedRepo, err := resolver.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, cachedRepo)
	})

	t.Run("uses repository spec with resolvers as fallback", func(t *testing.T) {
		repoSpec := &ociv1.Repository{
			Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
			BaseUrl: "localhost:5000/fallback",
		}

		opts := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config-resolvers",
					},
				},
			},
			Namespace: "default",
		}

		cachedRepo, err := resolver.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, cachedRepo)
	})

	t.Run("uses direct repository when no resolvers configured", func(t *testing.T) {
		// Create a config without resolvers
		emptyConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ocm-config-empty",
				Namespace: "default",
			},
			Data: map[string]string{
				".ocmconfig": `{
					"type": "generic.config.ocm.software/v1",
					"configurations": [
						{
							"type": "credentials.config.ocm.software",
							"repositories": []
						}
					]
				}`,
			},
		}

		k8sClient2 := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(emptyConfigMap).
			Build()

		cache2 := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
		wp2 := workerpool.NewWorkerPool(workerpool.PoolOptions{
			Logger: logger,
			Client: k8sClient2,
			Cache:  cache2,
		})

		resolver2 := resolution.NewResolver(k8sClient2, logger, wp2, pm)

		repoSpec := &ociv1.Repository{
			Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
			BaseUrl: "localhost:5000/direct",
		}

		opts := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config-empty",
					},
				},
			},
			Namespace: "default",
		}

		cachedRepo, err := resolver2.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, cachedRepo)
	})
}

// Test plugin implementation
type testOCIPlugin struct {
	component string
	version   string
}

func (p *testOCIPlugin) Name() string {
	return "oci"
}

func (p *testOCIPlugin) GetComponentVersionRepositoryScheme() *ocmruntime.Scheme {
	s := ocmruntime.NewScheme()
	s.MustRegisterWithAlias(&ociv1.Repository{},
		ocmruntime.NewVersionedType("oci", "v1"),
		ocmruntime.NewUnversionedType("oci"),
	)
	return s
}

func (p *testOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (ocmrepository.ComponentVersionRepository, error) {
	return &mockTestRepository{
		component: p.component,
		version:   p.version,
	}, nil
}

func (p *testOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
	_ context.Context,
	_ ocmruntime.Typed,
) (ocmruntime.Identity, error) {
	return nil, nil
}

type mockTestRepository struct {
	ocmrepository.ComponentVersionRepository
	component string
	version   string
}

func (m *mockTestRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    m.component,
					Version: m.version,
				},
			},
		},
	}, nil
}

func (m *mockTestRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return []string{m.version}, nil
}

func registerTestOCIPlugin(t *testing.T, pm *manager.PluginManager, component, version string) {
	t.Helper()

	plugin := &testOCIPlugin{
		component: component,
		version:   version,
	}

	err := pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(plugin)
	require.NoError(t, err)
}
