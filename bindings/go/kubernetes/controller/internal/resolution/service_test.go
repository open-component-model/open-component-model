package resolution_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func TestResolveComponentVersion_Success(t *testing.T) {
	ctx := context.Background()
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

	env := setupTestEnvironment(t, &logger)
	t.Cleanup(func() {
		err := env.Close(ctx)
		require.NoError(t, err)
	})

	repoSpec := &ociv1.Repository{
		Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
		BaseUrl: "localhost:5000/test",
	}

	cfg, err := configuration.LoadConfigurations(ctx, k8sClient, "default", []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "ocm-config",
			},
		},
	})
	require.NoError(t, err)

	opts := &resolution.Options{
		RepositorySpec: repoSpec,
		Configuration:  cfg,
		PluginManager:  env.PluginManager,
	}

	resolvedResult, err := env.Resolver.GetComponentVersion(ctx, opts, resolution.Verification{}, "test-component", "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, resolvedResult)
	assert.Equal(t, "test-component", resolvedResult.Component.Name)
	assert.Equal(t, "v1.0.0", resolvedResult.Component.Version)
	assert.NotZero(t, resolvedResult)
}

func TestResolveComponentVersion_DifferentConfigs(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	configMap1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-config-1",
			Namespace: "default",
		},
		Data: map[string]string{
			".ocmconfig": `{
			"type": "generic.config.ocm.software/v1",
			"configurations": []
		}`,
		},
	}

	configMap2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-config-2",
			Namespace: "default",
		},
		Data: map[string]string{
			".ocmconfig": `{
			"type": "generic.config.ocm.software/v1",
			"configurations": [
				{
					"type": "credentials.config.ocm.software/v1",
					"repositories": []
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
		WithObjects(configMap1, configMap2).
		Build()

	env := setupTestEnvironment(t, &logger)
	t.Cleanup(func() {
		err := env.Close(ctx)
		require.NoError(t, err)
	})

	repoSpec := &ociv1.Repository{
		Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
		BaseUrl: "localhost:5000/test",
	}

	// First call with config1
	cfg1, err := configuration.LoadConfigurations(ctx, k8sClient, "default", []v1alpha1.OCMConfiguration{
		{
			Kind: "ConfigMap",
			Name: "ocm-config-1",
		},
	})
	require.NoError(t, err)

	opts1 := &resolution.Options{
		RepositorySpec: repoSpec,
		Configuration:  cfg1,
		PluginManager:  env.PluginManager,
	}

	result1, err := env.Resolver.GetComponentVersion(ctx, opts1, resolution.Verification{}, "test-component", "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, result1)

	cfg2, err := configuration.LoadConfigurations(ctx, k8sClient, "default", []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "ocm-config-2",
			},
		},
	})
	require.NoError(t, err)

	opts2 := &resolution.Options{
		RepositorySpec: repoSpec,
		Configuration:  cfg2,
		PluginManager:  env.PluginManager,
	}

	result2, err := env.Resolver.GetComponentVersion(ctx, opts2, resolution.Verification{}, "test-component", "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, result2)
}

func TestResolveComponentVersion_MissingConfig(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	env := setupTestEnvironment(t, &logger)
	t.Cleanup(func() {
		err := env.Close(ctx)
		require.NoError(t, err)
	})

	repoSpec := &ociv1.Repository{
		BaseUrl: "localhost:5000/test",
	}

	_, err := configuration.LoadConfigurations(ctx, k8sClient, "default", []v1alpha1.OCMConfiguration{
		{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				Kind: "ConfigMap",
				Name: "missing-config",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get ConfigMap default/missing-config")

	// Also verify that passing nil Configuration (no configs) works without error.
	opts := &resolution.Options{
		RepositorySpec: repoSpec,
		Configuration:  nil,
		PluginManager:  env.PluginManager,
	}

	_, err = env.Resolver.RepositoryResolver(ctx, opts)
	require.NoError(t, err)
}

// testEnvironment holds the test infrastructure including resolver and plugin manager.
type testEnvironment struct {
	Resolver      *resolution.Resolver
	PluginManager *manager.PluginManager
}

func (e *testEnvironment) Close(ctx context.Context) error {
	if e.PluginManager != nil {
		return e.PluginManager.Shutdown(ctx)
	}

	return nil
}

// setupTestEnvironment creates a test environment with a resolver that has mock plugins registered.
func setupTestEnvironment(t *testing.T, logger *logr.Logger) *testEnvironment {
	t.Helper()

	cvRepoPlugin := &mockPlugin{
		component: "test-component",
		version:   "v1.0.0",
	}

	pm := manager.NewPluginManager(t.Context())
	err := pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(
		cvRepoPlugin,
	)
	require.NoError(t, err)

	return &testEnvironment{
		Resolver:      resolution.NewResolver(logger),
		PluginManager: pm,
	}
}

// The manager is derived from the configuration and is what the resolver cache
// key implicitly assumes, so callers must always supply one.
func TestRepositoryResolverRequiresPluginManager(t *testing.T) {
	logger := logr.Discard()

	env := setupTestEnvironment(t, &logger)
	t.Cleanup(func() {
		require.NoError(t, env.Close(t.Context()))
	})

	repoSpec := &ociv1.Repository{
		Type:    ocmruntime.Type{Name: "oci", Version: "v1"},
		BaseUrl: "localhost:5000/test",
	}

	_, err := env.Resolver.RepositoryResolver(t.Context(), &resolution.Options{
		RepositorySpec: repoSpec,
		Configuration:  nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin manager is required")
}

// routingPlugin is a spec-aware OCI repository plugin: the repository it hands
// out tags every descriptor with the BaseUrl of the spec it was built from, so
// tests can assert which repository actually served a component.
type routingPlugin struct{}

var _ repository.ComponentVersionRepositoryProvider = (*routingPlugin)(nil)

func (p *routingPlugin) GetJSONSchemaForRepositorySpecification(ocmruntime.Type) ([]byte, error) {
	return nil, nil
}

func (p *routingPlugin) GetComponentVersionRepositoryScheme() *ocmruntime.Scheme {
	return ocirepository.Scheme
}

func (p *routingPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, spec ocmruntime.Typed) (ocmruntime.Identity, error) {
	ociRepoSpec, err := asOCIRepository(spec)
	if err != nil {
		return nil, err
	}
	identity, err := ocmruntime.ParseURLToIdentity(ociRepoSpec.BaseUrl)
	if err != nil {
		return nil, err
	}
	identity.SetType(ocmruntime.NewVersionedType(ociv1.Type, ociv1.Version))
	return identity, nil
}

func (p *routingPlugin) GetComponentVersionRepository(_ context.Context, spec ocmruntime.Typed, _ ocmruntime.Typed) (repository.ComponentVersionRepository, error) {
	ociRepoSpec, err := asOCIRepository(spec)
	if err != nil {
		return nil, err
	}
	return &routingRepo{baseURL: ociRepoSpec.BaseUrl}, nil
}

// asOCIRepository decodes both typed and raw OCI repository specs.
func asOCIRepository(spec ocmruntime.Typed) (*ociv1.Repository, error) {
	if r, ok := spec.(*ociv1.Repository); ok {
		return r, nil
	}
	r := &ociv1.Repository{}
	if err := ocirepository.Scheme.Convert(spec, r); err != nil {
		return nil, fmt.Errorf("invalid repository specification: %w", err)
	}
	return r, nil
}

type routingRepo struct {
	repository.ComponentVersionRepository
	baseURL string
}

func (r *routingRepo) GetComponentVersion(_ context.Context, component, version string) (*descriptor.Descriptor, error) {
	d := &descriptor.Descriptor{}
	d.Component.Name = component
	d.Component.Version = version
	// Tag the resolved descriptor with the repository that served it.
	d.Component.Provider.Name = r.baseURL
	return d, nil
}

// A configured path matcher must still override the base repository: the
// migrated createResolver caller must not inject a high-priority root pattern
// for the base repository (that would shadow the configured matcher).
func TestResolveComponentVersion_ConfiguredMatcherOverridesBaseRepository(t *testing.T) {
	ctx := context.Background()

	const routedURL = "localhost:5000/routed"
	const baseURL = "localhost:5000/base"

	ocmConfig := fmt.Sprintf(`{
		"type": "generic.config.ocm.software/v1",
		"configurations": [{
			"type": "resolvers.config.ocm.software/v1alpha1",
			"resolvers": [{
				"repository": {"type": "OCIRepository/v1", "baseUrl": %q},
				"componentNamePattern": "test-component"
			}]
		}]
	}`, routedURL)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ocm-config", Namespace: "default"},
		Data:       map[string]string{".ocmconfig": ocmConfig},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	logr := logr.Discard()
	pm := manager.NewPluginManager(t.Context())
	require.NoError(t, pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(&routingPlugin{}))

	resolver := resolution.NewResolver(&logr)
	t.Cleanup(func() { require.NoError(t, pm.Shutdown(ctx)) })

	baseSpec := &ociv1.Repository{
		Type:    ocmruntime.NewVersionedType(ociv1.Type, ociv1.Version),
		BaseUrl: baseURL,
	}

	cfg, err := configuration.LoadConfigurations(ctx, k8sClient, "default", []v1alpha1.OCMConfiguration{{
		NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: "ocm-config"},
	}})
	require.NoError(t, err)

	result, err := resolver.GetComponentVersion(ctx, &resolution.Options{
		RepositorySpec: baseSpec,
		Configuration:  cfg,
		PluginManager:  pm,
	}, resolution.Verification{}, "test-component", "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, routedURL, result.Component.Provider.Name,
		"configured matcher must route to the configured repository, not the base repository")
}

// mockPlugin is a minimal OCI repository plugin for testing.
// It implements both the plugin interface and the repository interface.
type mockPlugin struct {
	repository.ComponentVersionRepository
	component string
	version   string
}

func (p *mockPlugin) GetJSONSchemaForRepositorySpecification(typ ocmruntime.Type) ([]byte, error) {
	return nil, nil
}

func (p *mockPlugin) GetComponentVersionRepositoryScheme() *ocmruntime.Scheme {
	return ocirepository.Scheme
}

var _ repository.ComponentVersionRepositoryProvider = (*mockPlugin)(nil)

func (p *mockPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *mockPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ ocmruntime.Typed,
) (repository.ComponentVersionRepository, error) {
	// Return the plugin itself as it implements the repository interface
	return p, nil
}

// GetComponentVersion implements repository.ComponentVersionRepository
func (p *mockPlugin) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    p.component,
					Version: p.version,
				},
			},
		},
	}, nil
}
