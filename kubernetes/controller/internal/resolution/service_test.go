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
	"ocm.software/open-component-model/kubernetes/controller/internal/plugins"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
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

		env := setupTestEnvironment(t, k8sClient, logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.ResolveOptions{
			RepositorySpec: repoSpec,
			Component:      "test-component",
			Version:        "v1.0.0",
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

		result, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "expected in-progress error on first call")

		synctest.Wait()

		resolvedResult, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, resolvedResult)
		assert.NotNil(t, resolvedResult.Descriptor)
		assert.NotNil(t, resolvedResult.Repository)
		assert.Equal(t, "test-component", resolvedResult.Descriptor.Component.Name)
		assert.Equal(t, "v1.0.0", resolvedResult.Descriptor.Component.Version)
		assert.NotZero(t, resolvedResult)
		assert.NotEmpty(t, resolvedResult.ConfigHash)
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

		env := setupTestEnvironment(t, k8sClient, logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.ResolveOptions{
			RepositorySpec: repoSpec,
			Component:      "test-component",
			Version:        "v1.0.0",
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

		result1, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		assert.Nil(t, result1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress), "first call should be in progress")

		synctest.Wait()

		result1, err = env.Resolver.ResolveComponentVersion(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, result1)

		result2, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Results should be identical (same pointer from cache)
		assert.Equal(t, result1.Descriptor.Component.Name, result2.Descriptor.Component.Name)
		assert.Equal(t, result1.Descriptor.Component.Version, result2.Descriptor.Component.Version)
		assert.Equal(t, result1.ConfigHash, result2.ConfigHash)
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

		env := setupTestEnvironment(t, k8sClient, logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		// First call with config1
		opts1 := &resolution.ResolveOptions{
			RepositorySpec: repoSpec,
			Component:      "test-component",
			Version:        "v1.0.0",
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

		result1, err := env.Resolver.ResolveComponentVersion(ctx, opts1)
		assert.Nil(t, result1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result1, err = env.Resolver.ResolveComponentVersion(ctx, opts1)
		require.NoError(t, err)
		require.NotNil(t, result1)

		opts2 := &resolution.ResolveOptions{
			RepositorySpec: repoSpec,
			Component:      "test-component",
			Version:        "v1.0.0",
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

		result2, err := env.Resolver.ResolveComponentVersion(ctx, opts2)
		assert.Nil(t, result2)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result2, err = env.Resolver.ResolveComponentVersion(ctx, opts2)
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Config hashes should be different
		assert.NotEqual(t, result1.ConfigHash, result2.ConfigHash)
	})
}

func TestResolveComponentVersion_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// Use TTL=0 to avoid background ticker goroutine
	cache := expirable.NewLRU[string, *resolution.Result](0, nil, 0)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		Cache: cache,
	})

	resolver := resolution.NewResolver(k8sClient, logger, wp)

	repoSpec := &ociv1.Repository{
		BaseUrl: "localhost:5000/test",
	}

	tests := []struct {
		name    string
		opts    *resolution.ResolveOptions
		wantErr string
	}{
		{
			name:    "nil options",
			opts:    nil,
			wantErr: "resolve options cannot be nil",
		},
		{
			name: "missing repository spec",
			opts: &resolution.ResolveOptions{
				Component: "test",
				Version:   "v1.0.0",
			},
			wantErr: "repository spec is required",
		},
		{
			name: "missing component name",
			opts: &resolution.ResolveOptions{
				RepositorySpec: repoSpec,
				Version:        "v1.0.0",
			},
			wantErr: "component name is required",
		},
		{
			name: "missing version",
			opts: &resolution.ResolveOptions{
				RepositorySpec: repoSpec,
				Component:      "test",
			},
			wantErr: "component version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.ResolveComponentVersion(ctx, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
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

	// Use TTL=0 to avoid background ticker goroutine
	cache := expirable.NewLRU[string, *resolution.Result](0, nil, 0)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		Client: k8sClient,
		Cache:  cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp)

	repoSpec := &ociv1.Repository{
		BaseUrl: "localhost:5000/test",
	}

	opts := &resolution.ResolveOptions{
		RepositorySpec: repoSpec,
		Component:      "test-component",
		Version:        "v1.0.0",
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

	_, err := resolver.ResolveComponentVersion(ctx, opts)
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

		env := setupTestEnvironment(t, k8sClient, logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		repoSpec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		opts := &resolution.ResolveOptions{
			RepositorySpec: repoSpec,
			Component:      "test-component",
			Version:        "v1.0.0",
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

		const numGoroutines = 10
		results := make([]*resolution.ResolveResult, numGoroutines)
		errs := make([]error, numGoroutines)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		// Fire off concurrent requests
		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				result, err := env.Resolver.ResolveComponentVersion(ctx, opts)
				results[idx] = result
				errs[idx] = err
			}(i)
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

		finalResult, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		require.NoError(t, err)
		require.NotNil(t, finalResult)

		for range numGoroutines {
			result, err := env.Resolver.ResolveComponentVersion(ctx, opts)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, finalResult.Descriptor.Component.Name, result.Descriptor.Component.Name)
			assert.Equal(t, finalResult.Descriptor.Component.Version, result.Descriptor.Component.Version)
		}
	})
}

// testEnvironment holds the test infrastructure including resolver and plugin manager.
type testEnvironment struct {
	Resolver      resolution.ComponentVersionResolver
	PluginManager *manager.PluginManager
}

func (e *testEnvironment) Close(ctx context.Context) error {
	if e.PluginManager != nil {
		return e.PluginManager.Shutdown(ctx)
	}

	return nil
}

// setupTestEnvironment creates a test environment with a resolver that has mock plugins registered.
func setupTestEnvironment(t *testing.T, k8sClient client.Reader, logger logr.Logger) *testEnvironment {
	t.Helper()

	// Register mock OCI plugin
	scheme := ocmruntime.NewScheme()
	ocirepository.MustAddToScheme(scheme)

	cvRepoPlugin := &mockOCIPlugin{
		component: "test-component",
		version:   "v1.0.0",
	}

	pm := manager.NewPluginManager(t.Context())
	err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		scheme,
		pm.ComponentVersionRepositoryRegistry,
		cvRepoPlugin,
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	cache := expirable.NewLRU[string, *resolution.Result](0, nil, 0)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		PluginManager: &plugins.PluginManager{
			PluginManager: pm,
		},
		Logger: logger,
		Client: k8sClient,
		Cache:  cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp)

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

// mockOCIPlugin is a minimal OCI repository plugin for testing.
type mockOCIPlugin struct {
	component string
	version   string
}

func (p *mockOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *mockOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	return &mockRepository{
		component: p.component,
		version:   p.version,
	}, nil
}

// mockRepository is a minimal repository implementation for testing.
type mockRepository struct {
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
