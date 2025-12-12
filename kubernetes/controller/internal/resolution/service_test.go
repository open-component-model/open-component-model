package resolution_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

func TestResolveComponentVersion_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, &logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config",
					},
				},
			},
			Namespace: "default",
		}

		repo, err := env.Resolver.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)

		result, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "expected in-progress error on first call")

		synctest.Wait()

		resolvedResult, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, resolvedResult)
		assert.Equal(t, "test-component", resolvedResult.Component.Name)
		assert.Equal(t, "v1.0.0", resolvedResult.Component.Version)
		assert.NotZero(t, resolvedResult)
	})
}

func TestResolveComponentVersion_CacheHit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, &logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config",
					},
				},
			},
			Namespace: "default",
		}

		repo, err := env.Resolver.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)

		result1, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		assert.Nil(t, result1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "first call should be in progress")

		synctest.Wait()

		result1, err = repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, result1)

		result2, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, result2)

		assert.Equal(t, result1.Component.Name, result2.Component.Name)
		assert.Equal(t, result1.Component.Version, result2.Component.Version)
	})
}

func TestResolveComponentVersion_CacheMissOnConfigChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, &logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		// First call with config1
		opts1 := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config-1",
					},
				},
			},
			Namespace: "default",
		}

		repo1, err := env.Resolver.NewCacheBackedRepository(ctx, opts1)
		require.NoError(t, err)

		result1, err := repo1.GetComponentVersion(ctx, "test-component", "v1.0.0")
		assert.Nil(t, result1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "first call should be in progress")

		synctest.Wait()

		result1, err = repo1.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, result1)

		opts2 := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config-2",
					},
				},
			},
			Namespace: "default",
		}

		repo2, err := env.Resolver.NewCacheBackedRepository(ctx, opts2)
		require.NoError(t, err)

		result2, err := repo2.GetComponentVersion(ctx, "test-component", "v1.0.0")
		assert.Nil(t, result2)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "first call should be in progress")

		synctest.Wait()

		result2, err = repo2.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, result2)
	})
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

	env := setupTestEnvironment(t, k8sClient, &logger)
	t.Cleanup(func() {
		err := env.Close(ctx)
		require.NoError(t, err)
	})

	repoSpec := &ociv1.Repository{
		BaseUrl: "localhost:5000/test",
	}

	opts := &resolution.RepositoryOptions{
		RepositorySpec: repoSpec,
		OCMConfigurations: []v1alpha1.OCMConfiguration{
			{
				NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
					Kind: "ConfigMap",
					Name: "missing-config",
				},
			},
		},
		Namespace: "default",
	}

	_, err := env.Resolver.NewCacheBackedRepository(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load OCM configurations")
}

func TestResolveComponentVersionDeduplication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, &logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.RepositoryOptions{
			RepositorySpec: repoSpec,
			OCMConfigurations: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "ocm-config",
					},
				},
			},
			Namespace: "default",
		}

		repo, err := env.Resolver.NewCacheBackedRepository(ctx, opts)
		require.NoError(t, err)

		const numGoroutines = 10
		results := make([]*descriptor.Descriptor, numGoroutines)
		errs := make([]error, numGoroutines)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		// Fire off concurrent requests
		for i := range numGoroutines {
			go func() {
				defer wg.Done()
				result, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
				results[i] = result
				errs[i] = err
			}()
		}

		wg.Wait()

		inProgressCount := 0
		successCount := 0
		for i := range numGoroutines {
			if errors.Is(errs[i], resolution.ErrResolutionInProgress) {
				inProgressCount++
			} else if errs[i] == nil {
				successCount++
			}
		}

		// With inProgress tracking, the first request will enqueue work.
		// Remaining requests should either:
		// - See it's in progress and get ErrResolutionInProgress, OR
		// - Come after completion and get cached result
		// We verify deduplication worked by checking all either succeeded or got in-progress
		assert.Equal(t, inProgressCount+successCount, numGoroutines, "all requests should either succeed or get in-progress")
		assert.Greater(t, inProgressCount, 0, "at least some goroutines should get in-progress before completion")

		synctest.Wait()

		finalResult, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
		require.NoError(t, err)
		require.NotNil(t, finalResult)

		for range numGoroutines {
			result, err := repo.GetComponentVersion(ctx, "test-component", "v1.0.0")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, finalResult.Component.Name, result.Component.Name)
			assert.Equal(t, finalResult.Component.Version, result.Component.Version)
		}
	})
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
func setupTestEnvironment(t *testing.T, k8sClient client.Reader, logger *logr.Logger) *testEnvironment {
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

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		Logger: logger,
		Client: k8sClient,
		Cache:  cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp, pm)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	// Start worker pool in background since Start() blocks for graceful shutdown
	go func() {
		_ = wp.Start(ctx)
	}()

	return &testEnvironment{
		Resolver:      resolver,
		PluginManager: pm,
	}
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
	_ map[string]string,
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
