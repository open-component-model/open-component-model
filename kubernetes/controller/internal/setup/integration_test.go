// Package setup_test contains a very lightweight integration test that uses the configuration package
// and the context package together.
package setup_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/provider"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

func TestIntegration_CompleteFlow(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-config",
			Namespace: "default",
		},
		Data: map[string]string{
			".ocmconfig": `{
				"type": "generic.config.ocm.software/v1",
				"configurations": []
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

	ocmConfigs := []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "ocm-config",
			},
		},
	}

	cfg, err := configuration.LoadConfigurations(ctx, k8sClient, "default", ocmConfigs)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	pm := manager.NewPluginManager(t.Context())
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(t.Context()))
	})

	registerOCIPlugin(t, pm, "test", "v1.0.0")

	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: pm,
		Logger:        &logger,
	})
	require.NoError(t, err)
	require.NotNil(t, credGraph)

	// Test new path matcher resolvers (v1alpha1)
	resolversV1Alpha1, err := provider.PathMatcherResolversFromConfig(cfg.Config)
	require.NoError(t, err)
	assert.Nil(t, resolversV1Alpha1, "empty config should have no resolvers")

	t.Run("repository creation", func(t *testing.T) {
		opts := setup.RepositoryOptions{
			Registry:        pm.ComponentVersionRepositoryRegistry,
			CredentialGraph: credGraph,
			Logger:          &logger,
		}

		// Create a simple OCI repository spec
		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		repo, err := setup.NewRepository(ctx, repoSpec, opts)
		require.NoError(t, err)
		cv, err := repo.GetComponentVersion(ctx, "test", "v1.0.0")
		require.NoError(t, err)
		assert.NotNil(t, cv)
		assert.Equal(t, "test:v1.0.0", cv.String())
	})
}

func TestIntegration_MultipleConfigSources(t *testing.T) {
	ctx := t.Context()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			".ocmconfig": []byte(`{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "credentials.config.ocm.software/v1",
						"repositories": [
							{
								"repository": {
									"type": "OCIRegistry",
									"hostname": "ghcr.io"
								},
								"credentials": [
									{
										"credentialsName": "secret-creds"
									}
								]
							}
						]
					}
				]
			}`),
		},
	}

	// Create ConfigMap with plugin config wrapped in generic config (in JSON format)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-plugins",
			Namespace: "default",
		},
		Data: map[string]string{
			".ocmconfig": `{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "plugin.config.ocm.software/v1",
						"plugins": [
							{
								"name": "helm",
								"version": "v1.0.0"
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
		WithObjects(secret, configMap).
		Build()

	// Load both configurations
	ocmConfigs := []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
				Kind: "Secret",
				Name: "ocm-creds",
			},
		},
		{
			NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "ocm-plugins",
			},
		},
	}

	cfg, err := configuration.LoadConfigurations(ctx, k8sClient, "default", ocmConfigs)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.NotNil(t, cfg)
	assert.Len(t, cfg.Config.Configurations, 2, "should have loaded 2 configurations (credentials + plugin)")
}

func TestIntegration_ErrorHandling(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	t.Run("missing config resource", func(t *testing.T) {
		scheme := runtime.NewScheme()
		require.NoError(t, corev1.AddToScheme(scheme))
		require.NoError(t, v1alpha1.AddToScheme(scheme))

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		ocmConfigs := []v1alpha1.OCMConfiguration{
			{
				NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
					Kind: "ConfigMap",
					Name: "missing-config",
				},
			},
		}

		_, err := configuration.LoadConfigurations(ctx, k8sClient, "default", ocmConfigs)
		assert.Error(t, err)
	})

	t.Run("nil plugin manager", func(t *testing.T) {
		_, err := setup.NewCredentialGraph(ctx, nil, setup.CredentialGraphOptions{
			PluginManager: nil,
			Logger:        &logger,
		})
		assert.Error(t, err)
	})
}

// registerOCIPlugin registers a mock OCI plugin for the test to find.
func registerOCIPlugin(t *testing.T, pm *manager.PluginManager, component, version string) {
	t.Helper()

	// Register a simple component version repository plugin
	cvRepoPlugin := &simpleOCIPlugin{
		component: component,
		version:   version,
	}

	err := pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(
		cvRepoPlugin,
	)

	require.NoError(t, err)
}

type mockRepository struct {
	// we don't care about the rest of the implementations right now other than GetComponentVersion
	repository.ComponentVersionRepository

	component string
	version   string
}

func (m *mockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
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

// simpleOCIPlugin is a minimal OCI repository plugin for testing.
type simpleOCIPlugin struct {
	component string
	version   string
}

func (p *simpleOCIPlugin) GetJSONSchemaForRepositorySpecification(typ ocmruntime.Type) ([]byte, error) {
	return nil, nil
}

func (p *simpleOCIPlugin) GetComponentVersionRepositoryScheme() *ocmruntime.Scheme {
	return ocirepository.Scheme
}

var _ repository.ComponentVersionRepositoryProvider = (*simpleOCIPlugin)(nil)

func (p *simpleOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
	_ context.Context,
	repositorySpecification ocmruntime.Typed,
) (ocmruntime.Identity, error) {
	ociRepoSpec, ok := repositorySpecification.(*ociv1.Repository)
	if !ok {
		return nil, fmt.Errorf("invalid repository specification: %T", repositorySpecification)
	}

	identity, err := ocmruntime.ParseURLToIdentity(ociRepoSpec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(ocmruntime.NewVersionedType(ociv1.Type, ociv1.Version))

	return identity, nil
}

func (p *simpleOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	// test plugin
	return &mockRepository{
		component: p.component,
		version:   p.version,
	}, nil
}

// TestIntegration_ResolverProvider tests the new path matcher resolver provider functionality.
func TestIntegration_ResolverProvider(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	// Create a config with path matcher resolvers
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-config-with-resolvers",
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
							},
							{
								"componentNamePattern": "ocm.software/*",
								"repository": {
									"type": "oci/v1",
									"baseUrl": "registry.io/ocm"
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

	ocmConfigs := []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "ocm-config-with-resolvers",
			},
		},
	}

	cfg, err := configuration.LoadConfigurations(ctx, k8sClient, "default", ocmConfigs)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	pm := manager.NewPluginManager(t.Context())
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(t.Context()))
	})

	registerOCIPlugin(t, pm, "github.com/test/myrepo", "v1.0.0")

	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: pm,
		Logger:        &logger,
	})
	require.NoError(t, err)

	t.Run("use resolver provider with repository and patterns", func(t *testing.T) {
		repoSpec := &ociv1.Repository{
			Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
			BaseUrl: "localhost:5000/priority",
		}

		configResolvers, err := provider.PathMatcherResolversFromConfig(cfg.Config)
		require.NoError(t, err)

		opts := provider.Options{
			RepoProvider:    pm.ComponentVersionRepositoryRegistry,
			CredentialGraph: credGraph,
			PathMatchers:    configResolvers,
		}

		p, err := provider.New(ctx, opts, repoSpec)
		require.NoError(t, err)
		require.NotNil(t, p)

		repo, err := p.GetComponentVersionRepositoryForComponent(ctx, "github.com/test/myrepo", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, repo)

		// TODO: Extend this if possible with actually trying to get the component.
	})
}
