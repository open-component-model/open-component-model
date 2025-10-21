package resolution_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
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
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/resolution"
)

func TestWorkerPool_NewWorkerPool_DefaultValues(t *testing.T) {
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{})

	require.NotNil(t, wp)
}

func TestWorkerPool_NewWorkerPool_CustomValues(t *testing.T) {
	logger := logr.Discard()

	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		WorkerCount: 5,
		QueueSize:   50,
		Logger:      logger,
	})

	require.NotNil(t, wp)
}

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

var _ logr.LogSink = &FakeLogger{}

func TestWorkerPool_StartAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeLogger := &FakeLogger{}
	logger := logr.New(fakeLogger)

	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		WorkerCount: 3,
		QueueSize:   10,
		Logger:      logger,
	})

	go func() { _ = wp.Start(ctx) }()

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop workers
	cancel()

	// Give workers time to stop
	time.Sleep(100 * time.Millisecond)

	// See if the worker quit
	assert.Contains(t, fakeLogger.GetLog(), "worker stopped")
}

func TestWorkerPool_SingleResolution(t *testing.T) {
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

	assert.Eventually(t, func() bool {
		result, err := env.Resolver.ResolveComponentVersion(ctx, opts)
		return err == nil && result != nil
	}, 2*time.Second, 50*time.Millisecond)
}

func TestWorkerPool_ParallelResolutions_DifferentComponents(t *testing.T) {
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

	resolver := setupDynamicTestEnvironment(t, k8sClient, logger)

	const numComponents = 20
	var wg sync.WaitGroup
	wg.Add(numComponents)

	results := make([]atomic.Pointer[resolution.ResolveResult], numComponents)
	errs := make([]atomic.Pointer[error], numComponents)

	for i := 0; i < numComponents; i++ {
		go func() {
			defer wg.Done()

			opts := &resolution.ResolveOptions{
				RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
				Component:      fmt.Sprintf("component-%d", i),
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

			// First call should return in-progress
			result, err := resolver.ResolveComponentVersion(ctx, opts)
			if result == nil && errors.Is(err, resolution.ErrResolutionInProgress) {
				// Poll until resolution completes
				for j := 0; j < 40; j++ {
					time.Sleep(50 * time.Millisecond)
					result, err = resolver.ResolveComponentVersion(ctx, opts)
					if result != nil || (err != nil && !errors.Is(err, resolution.ErrResolutionInProgress)) {
						break
					}
				}
			}

			results[i].Store(result)
			if err != nil {
				errs[i].Store(&err)
			}
		}()
	}

	wg.Wait()

	// Verify all resolutions completed successfully
	for i := 0; i < numComponents; i++ {
		result := results[i].Load()
		require.NotNil(t, result, "component-%d should have a result", i)
		assert.Equal(t, fmt.Sprintf("component-%d", i), result.Descriptor.Component.Name)
		assert.Equal(t, "v1.0.0", result.Descriptor.Component.Version)
	}
}

func TestWorkerPool_ParallelResolutions_SameComponent_Singleflight(t *testing.T) {
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

	// Use a mock plugin that tracks call count
	var callCount atomic.Int32
	countingPlugin := &countingOCIPlugin{
		component: "shared-component",
		version:   "v1.0.0",
		callCount: &callCount,
	}

	ocmScheme := ocmruntime.NewScheme()
	ocirepository.MustAddToScheme(ocmScheme)

	pm := manager.NewPluginManager(t.Context())
	err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmScheme,
		pm.ComponentVersionRepositoryRegistry,
		countingPlugin,
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	cache := resolution.NewInMemoryCache(30 * time.Second)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		WorkerCount:   5,
		QueueSize:     100,
		PluginManager: pm,
		Logger:        logger,
		Cache:         cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp)

	wpCtx, wpCancel := context.WithCancel(t.Context())
	t.Cleanup(wpCancel)

	go func() { _ = wp.Start(wpCtx) }()

	const numConcurrent = 50
	var wg sync.WaitGroup
	wg.Add(numConcurrent)

	results := make([]atomic.Pointer[resolution.ResolveResult], numConcurrent)
	errs := make([]atomic.Pointer[error], numConcurrent)

	// Fire off concurrent requests for the same component
	for i := range numConcurrent {
		go func() {
			defer wg.Done()

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

			result, err := resolver.ResolveComponentVersion(ctx, opts)
			if result == nil && errors.Is(err, resolution.ErrResolutionInProgress) {
				for range 40 {
					time.Sleep(50 * time.Millisecond)
					result, err = resolver.ResolveComponentVersion(ctx, opts)
					if result != nil || (err != nil && !errors.Is(err, resolution.ErrResolutionInProgress)) {
						break
					}
				}
			}

			results[i].Store(result)
			if err != nil {
				errs[i].Store(&err)
			}
		}()
	}

	wg.Wait()

	// Verify all requests got the same result
	for i := range numConcurrent {
		result := results[i].Load()
		require.NotNil(t, result, "request %d should have a result", i)
		assert.Equal(t, "shared-component", result.Descriptor.Component.Name)
	}

	// Singleflight prevents duplicate enqueues on the initial burst of requests.
	// However, callers poll after getting ErrResolutionInProgress, and if they
	// retry before the cache is populated, they might trigger another resolution.
	// With 50 concurrent goroutines polling, we expect significantly fewer calls
	// than 50, ideally just 1-2 but allowing some slack for timing.
	calls := callCount.Load()
	assert.LessOrEqual(t, calls, int32(15), "singleflight should reduce duplicate resolutions (got %d calls)", calls)
	t.Logf("Plugin was called %d times (singleflight deduplication working)", calls)
}

func TestWorkerPool_QueueFull(t *testing.T) {
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

	// Use a slow plugin to fill the queue
	slowPlugin := &slowOCIPlugin{
		component: "slow-component",
		version:   "v1.0.0",
		delay:     1 * time.Second,
	}

	ocmScheme := ocmruntime.NewScheme()
	ocirepository.MustAddToScheme(ocmScheme)

	pm := manager.NewPluginManager(t.Context())
	err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmScheme,
		pm.ComponentVersionRepositoryRegistry,
		slowPlugin,
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	cache := resolution.NewInMemoryCache(30 * time.Second)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		WorkerCount:   1, // Only 1 worker
		QueueSize:     2, // Very small queue
		PluginManager: pm,
		Logger:        logger,
		Cache:         cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp)

	wpCtx, wpCancel := context.WithCancel(t.Context())
	t.Cleanup(wpCancel)

	go func() { _ = wp.Start(wpCtx) }()

	// Try to enqueue more items than the queue can hold
	var queueFullCount atomic.Int32

	for i := range 10 {
		opts := &resolution.ResolveOptions{
			RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
			Component:      fmt.Sprintf("component-%d", i),
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

		_, err := resolver.ResolveComponentVersion(ctx, opts)
		if err != nil && !errors.Is(err, resolution.ErrResolutionInProgress) {
			if err.Error() == fmt.Sprintf("lookup queue is full, cannot enqueue request for component-%d:v1.0.0", i) {
				queueFullCount.Add(1)
			}
		}
	}

	// At least some requests should have been rejected due to full queue
	assert.Greater(t, queueFullCount.Load(), int32(0), "expected some requests to fail due to full queue")
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
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

	// Give workers time to stop
	time.Sleep(200 * time.Millisecond)
}

func TestWorkerPool_MultipleVersionsSameComponent(t *testing.T) {
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

	resolver := setupDynamicTestEnvironment(t, k8sClient, logger)

	versions := []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0"}
	const numConcurrent = 10

	var wg sync.WaitGroup
	wg.Add(len(versions) * numConcurrent)

	results := make(map[string][]atomic.Pointer[resolution.ResolveResult])
	for _, v := range versions {
		results[v] = make([]atomic.Pointer[resolution.ResolveResult], numConcurrent)
	}

	for _, version := range versions {
		for i := range numConcurrent {
			go func() {
				defer wg.Done()

				opts := &resolution.ResolveOptions{
					RepositorySpec: &ociv1.Repository{BaseUrl: "localhost:5000/test"},
					Component:      "multi-version-component",
					Version:        version,
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

				result, err := resolver.ResolveComponentVersion(ctx, opts)
				if result == nil && errors.Is(err, resolution.ErrResolutionInProgress) {
					for range 40 {
						time.Sleep(50 * time.Millisecond)
						result, err = resolver.ResolveComponentVersion(ctx, opts)
						if result != nil || (err != nil && !errors.Is(err, resolution.ErrResolutionInProgress)) {
							break
						}
					}
				}

				results[version][i].Store(result)
			}()
		}
	}

	wg.Wait()

	// Verify all versions resolved correctly
	for _, version := range versions {
		for i := range numConcurrent {
			result := results[version][i].Load()
			require.NotNil(t, result, "version %s, request %d should have a result", version, i)
			assert.Equal(t, "multi-version-component", result.Descriptor.Component.Name)
			assert.Equal(t, version, result.Descriptor.Component.Version)
		}
	}
}

func TestWorkerPool_CacheInvalidation(t *testing.T) {
	ctx := context.Background()
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
	var result1 *resolution.ResolveResult
	_, err := env.Resolver.ResolveComponentVersion(ctx, opts1)
	assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

	assert.Eventually(t, func() bool {
		result, err := env.Resolver.ResolveComponentVersion(ctx, opts1)
		if err == nil && result != nil {
			result1 = result
			return true
		}
		return false
	}, 2*time.Second, 50*time.Millisecond)

	require.NotNil(t, result1)
	hash1 := result1.ConfigHash

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
	var result2 *resolution.ResolveResult
	_, err = env.Resolver.ResolveComponentVersion(ctx, opts2)
	assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

	assert.Eventually(t, func() bool {
		result, err := env.Resolver.ResolveComponentVersion(ctx, opts2)
		if err == nil && result != nil {
			result2 = result
			return true
		}
		return false
	}, 2*time.Second, 50*time.Millisecond)

	require.NotNil(t, result2)
	hash2 := result2.ConfigHash

	// Different configs should produce different cache keys
	assert.NotEqual(t, hash1, hash2)
}

// countingOCIPlugin tracks how many times GetComponentVersionRepository is called
type countingOCIPlugin struct {
	component string
	version   string
	callCount *atomic.Int32
}

func (p *countingOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *countingOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	p.callCount.Add(1)
	time.Sleep(100 * time.Millisecond) // Simulate some work
	return &dynamicMockRepository{}, nil
}

// slowOCIPlugin simulates a slow repository operation
type slowOCIPlugin struct {
	component string
	version   string
	delay     time.Duration
}

func (p *slowOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *slowOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	time.Sleep(p.delay)
	return &dynamicMockRepository{}, nil
}

// dynamicMockRepository returns the requested component/version dynamically
type dynamicMockRepository struct {
	repository.ComponentVersionRepository
}

func (m *dynamicMockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
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

// dynamicOCIPlugin returns dynamic component/version values based on the request
type dynamicOCIPlugin struct{}

func (p *dynamicOCIPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(
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

func (p *dynamicOCIPlugin) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	return &dynamicMockRepository{}, nil
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
		&dynamicOCIPlugin{},
		&ociv1.Repository{},
	)
	require.NoError(t, err)

	cache := resolution.NewInMemoryCache(30 * time.Second)
	wp := resolution.NewWorkerPool(resolution.WorkerPoolOptions{
		PluginManager: pm,
		Logger:        logger,
		Client:        k8sClient,
		Cache:         cache,
	})
	resolver := resolution.NewResolver(k8sClient, logger, wp)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		_ = pm.Shutdown(ctx)
	})

	// Start worker pool in background since Start() blocks for graceful shutdown
	go func() {
		_ = wp.Start(ctx)
	}()
	
	// Give workers a moment to initialize
	time.Sleep(50 * time.Millisecond)

	return resolver
}
