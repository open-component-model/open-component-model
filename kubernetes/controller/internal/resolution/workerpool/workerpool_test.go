package workerpool_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
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
	"ocm.software/open-component-model/kubernetes/controller/internal/plugins"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
)

type FakeLogger struct {
	mu sync.Mutex
	logr.Logger
	infoBuffer  bytes.Buffer
	errorBuffer bytes.Buffer
}

func (logger *FakeLogger) Init(info logr.RuntimeInfo) {}
func (logger *FakeLogger) Enabled(lvl int) bool       { return true }
func (logger *FakeLogger) Info(lvl int, msg string, keysAndValues ...interface{}) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.infoBuffer.WriteString(msg)
}
func (logger *FakeLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.errorBuffer.WriteString(msg)
}
func (logger *FakeLogger) WithValues(keysAndValues ...interface{}) logr.LogSink { return logger }
func (logger *FakeLogger) WithName(name string) logr.LogSink                    { return logger }
func (logger *FakeLogger) GetLog() string {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	return logger.infoBuffer.String()
}

var _ logr.LogSink = (*FakeLogger)(nil)

func TestWorkerPool_StartAndStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		fakeLogger := &FakeLogger{}
		logger := logr.New(fakeLogger)

		wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
			WorkerCount: 3,
			QueueSize:   10,
			Logger:      logger,
		})

		go func() { _ = wp.Start(ctx) }()

		// Wait for workers to start
		synctest.Wait()

		// Cancel context to stop workers
		cancel()

		// Wait for workers to stop
		synctest.Wait()

		// See if the worker quit
		assert.Contains(t, fakeLogger.GetLog(), "worker stopped")
	})
}

func TestWorkerPool_SingleResolution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, logger)
		t.Cleanup(func() {
			err := env.Close(ctx)
			require.NoError(t, err)
		})

		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "single-component",
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
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		// Wait for all goroutines to become durably blocked (resolution complete)
		synctest.Wait()

		result, err = env.Resolver.ResolveComponentVersion(ctx, opts)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestWorkerPool_ParallelResolutions_DifferentComponents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		resolver := setupDynamicTestEnvironment(t, k8sClient, logger)

		const numComponents = 20
		results := make([]*resolution.ResolveResult, numComponents)
		errs := make([]error, numComponents)
		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "", // set in the iteration
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

		for i := range numComponents {
			go func() {
				o := *opts
				o.Component = fmt.Sprintf("component-%d", i)
				result, err := resolver.ResolveComponentVersion(ctx, &o)
				results[i] = result
				if err != nil {
					errs[i] = err
				}
			}()
		}

		// Wait for all resolutions to complete
		t.Logf("Starting %d components", numComponents)
		synctest.Wait()
		t.Log("wait finished")

		// Verify all resolutions completed successfully
		for i := range numComponents {
			result := results[i]
			if result == nil {
				// Try one more time after synctest.Wait
				opts.Component = fmt.Sprintf("component-%d", i)
				result, _ = resolver.ResolveComponentVersion(ctx, opts)
			}
			require.NotNil(t, result, "component-%d should have a result", i)
			assert.Equal(t, fmt.Sprintf("component-%d", i), result.Descriptor.Component.Name)
			assert.Equal(t, "v1.0.0", result.Descriptor.Component.Version)
		}
	})
}

func TestWorkerPool_ParallelResolutions_SameComponent_Deduplication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		// Use a plugin that tracks call count
		var callCount atomic.Int32
		plugin := &configurablePlugin{
			BeforeGetRepositoryFn: func() {
				callCount.Add(1)
			},
		}

		ocmScheme := ocmruntime.NewScheme()
		ocirepository.MustAddToScheme(ocmScheme)

		pm := manager.NewPluginManager(t.Context())
		err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
			ocmScheme,
			pm.ComponentVersionRepositoryRegistry,
			plugin,
			&ociv1.Repository{},
		)
		require.NoError(t, err)

		// Use TTL=0 to avoid background ticker goroutine that causes synctest deadlock
		cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
		wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
			WorkerCount: 5,
			QueueSize:   100,
			Logger:      logger,
			Cache:       cache,
		})
		resolver := resolution.NewResolver(k8sClient, logger, wp, &plugins.PluginManager{
			PluginManager: pm,
		})

		wpCtx, wpCancel := context.WithCancel(t.Context())
		t.Cleanup(wpCancel)

		go func() { _ = wp.Start(wpCtx) }()

		const numConcurrent = 50
		results := make([]*resolution.ResolveResult, numConcurrent)
		errs := make([]error, numConcurrent)

		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "shared-component",
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

		// Fire off concurrent requests for the same component
		for i := range numConcurrent {
			go func() {
				result, err := resolver.ResolveComponentVersion(ctx, opts)
				results[i] = result
				if err != nil {
					errs[i] = err
				}
			}()
		}

		t.Log("before synctest wait")
		// Wait for resolution to complete
		synctest.Wait()
		t.Log("after synctest wait")

		// Verify all requests got the result
		for i := range numConcurrent {
			result := results[i]
			if result == nil {
				// refetch things after synctest wait is done
				result, err = resolver.ResolveComponentVersion(ctx, opts)
				require.NoError(t, err)
			}
			require.NotNil(t, result, "request %d should have a result", i)
			assert.Equal(t, "shared-component", result.Descriptor.Component.Name)
		}

		calls := callCount.Load()
		assert.Equal(t, calls, int32(1), "inProgress tracking should allow only a single call to the plugin (got %d calls)", calls)
		t.Logf("plugin was called %d times (inProgress deduplication working)", calls)
	})
}

func TestWorkerPool_QueueFull(t *testing.T) {
	// Note: This test uses real time.Sleep and is NOT wrapped in synctest.Test
	// because it tests queue overflow behavior with a slow plugin that blocks workers.
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

	// Use a slow plugin to fill the queue
	plugin := &configurablePlugin{
		BeforeGetRepositoryFn: func() {
			time.Sleep(5 * time.Second)
		},
	}

	ocmScheme := ocmruntime.NewScheme()
	ocirepository.MustAddToScheme(ocmScheme)

	pm := manager.NewPluginManager(t.Context())
	err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmScheme,
		pm.ComponentVersionRepositoryRegistry,
		plugin,
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: 1, // Only 1 worker
		QueueSize:   2, // Very small queue
		Logger:      logger,
		Cache:       cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp, &plugins.PluginManager{
		PluginManager: pm,
	})

	wpCtx, wpCancel := context.WithCancel(t.Context())
	t.Cleanup(wpCancel)

	go func() { _ = wp.Start(wpCtx) }()

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Try to enqueue more items than the queue can hold - fire concurrently
	var queueFullCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(10)

	opts := &resolution.ResolveOptions{
		RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
		Component:      "", // set during the for loop
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

	for i := range 10 {
		go func() {
			defer wg.Done()
			o := *opts
			o.Component = fmt.Sprintf("component-%d", i)
			_, err := resolver.ResolveComponentVersion(ctx, &o)
			if err != nil && !errors.Is(err, resolution.ErrResolutionInProgress) {
				if err.Error() == fmt.Sprintf("work queue is full, cannot enqueue request for component-%d:v1.0.0", i) {
					queueFullCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// 10 different components, queue size 2, and 1 worker:
	// - 1 worker picks up first item
	// - 2 items are queued
	// - 3 can be accepted, so 7 should fail with queue full
	assert.GreaterOrEqual(t, queueFullCount.Load(), int32(7), "expected at least 7 requests to fail due to full queue (got %d)", queueFullCount.Load())
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
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

		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "cancel-component",
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

		// Start resolution
		result, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		// Cancel context immediately
		cancel()

		// Close environment (stops worker pool)
		err = env.Close(ctx)
		require.NoError(t, err)

		// Wait for workers to stop
		synctest.Wait()
	})
}

func TestWorkerPool_MultipleVersionsSameComponent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		resolver := setupDynamicTestEnvironment(t, k8sClient, logger)

		versions := []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0"}
		const numConcurrent = 10

		results := make(map[string][]*resolution.ResolveResult)
		for _, v := range versions {
			results[v] = make([]*resolution.ResolveResult, numConcurrent)
		}
		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "multi-version-component",
			Version:        "", // set during the loops
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

		for _, version := range versions {
			for i := range numConcurrent {
				go func() {
					o := *opts
					o.Version = version
					result, _ := resolver.ResolveComponentVersion(ctx, &o)
					results[version][i] = result
				}()
			}
		}

		t.Log("before the synctest.Wait call")
		// Wait for all resolutions to complete
		synctest.Wait()
		t.Log("after the synctest.Wait call")

		// Verify all versions resolved correctly
		for _, version := range versions {
			for i := range numConcurrent {
				result := results[version][i]
				if result == nil {
					// refetch values after sync wait is done for all go routines
					opts.Version = version
					var err error
					result, err = resolver.ResolveComponentVersion(ctx, opts)
					require.NoError(t, err)
				}
				require.NotNil(t, result, "version %s, request %d should have a result", version, i)
				assert.Equal(t, "multi-version-component", result.Descriptor.Component.Name)
				assert.Equal(t, version, result.Descriptor.Component.Version)
			}
		}
	})
}

func TestWorkerPool_CacheInvalidation(t *testing.T) {
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

		opts1 := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "cache-test",
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

		// First resolution with config-1
		_, err := env.Resolver.ResolveComponentVersion(ctx, opts1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result1, err := env.Resolver.ResolveComponentVersion(ctx, opts1)
		require.NoError(t, err)
		require.NotNil(t, result1)

		opts2 := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      "cache-test",
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

		// Second resolution with config-2 (different config = cache miss)
		_, err = env.Resolver.ResolveComponentVersion(ctx, opts2)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result2, err := env.Resolver.ResolveComponentVersion(ctx, opts2)
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Different configs should produce different cache entries (both should succeed)
		assert.Equal(t, result1.Descriptor.Component.Name, result2.Descriptor.Component.Name)
		assert.Equal(t, result1.Descriptor.Component.Version, result2.Descriptor.Component.Version)
	})
}

// configurablePlugin is a flexible mock plugin for testing that allows customizing behavior.
type configurablePlugin struct {
	repository.ComponentVersionRepository
	GetComponentVersionFn func(ctx context.Context, component, version string) (*descriptor.Descriptor, error)
	BeforeGetRepositoryFn func()
}

func (p *configurablePlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *configurablePlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	if p.BeforeGetRepositoryFn != nil {
		p.BeforeGetRepositoryFn()
	}

	return p, nil
}

// GetComponentVersion implements repository.ComponentVersionRepository
func (p *configurablePlugin) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	if p.GetComponentVersionFn != nil {
		return p.GetComponentVersionFn(ctx, component, version)
	}

	// Default behavior: return requested component/version dynamically
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

// setupDynamicTestEnvironment creates a test environment with dynamic plugin that returns requested component/version
func setupDynamicTestEnvironment(t *testing.T, k8sClient client.Reader, logger logr.Logger) resolution.ComponentVersionResolver {
	t.Helper()

	ocmScheme := ocmruntime.NewScheme()
	ocirepository.MustAddToScheme(ocmScheme)

	pm := manager.NewPluginManager(t.Context())
	err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmScheme,
		pm.ComponentVersionRepositoryRegistry,
		&configurablePlugin{},
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	// Use TTL=0 to avoid background ticker goroutine that causes synctest deadlock
	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		Logger: logger,
		Client: k8sClient,
		Cache:  cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp, &plugins.PluginManager{
		PluginManager: pm,
	})

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		_ = pm.Shutdown(ctx)
	})

	// Start worker pool in background since Start() blocks for graceful shutdown
	go func() {
		_ = wp.Start(ctx)
	}()

	return resolver
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

	cvRepoPlugin := &mockPlugin{
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

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
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

// mockPlugin is a minimal OCI repository plugin for testing.
// It implements both the plugin interface and the repository interface.
type mockPlugin struct {
	repository.ComponentVersionRepository
	component string
	version   string
}

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
