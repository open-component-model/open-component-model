package workerpool_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

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
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
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
			Logger:      &logger,
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		opts := workerpool.ResolveOptions{
			Component:  "single-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "ocm-config", nil },
			Repository: &mockRepository{},
		}

		result, err := env.Pool.GetComponentVersion(ctx, opts)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		// Wait for all goroutines to become durably blocked (resolution complete)
		synctest.Wait()

		result, err = env.Pool.GetComponentVersion(ctx, opts)
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		const numComponents = 20
		results := make([]*descriptor.Descriptor, numComponents)

		// Create a single shared mock repository to avoid race conditions
		mockRepo := &mockRepository{}

		for i := range numComponents {
			go func() {
				opts := workerpool.ResolveOptions{
					Component:  fmt.Sprintf("component-%d", i),
					Version:    "v1.0.0",
					KeyFunc:    func() (string, error) { return fmt.Sprintf("ocm-config-%d", i), nil },
					Repository: mockRepo,
				}
				// we ignore the error since it's InProgress error.
				// we check for the results later on.
				result, _ := env.Pool.GetComponentVersion(ctx, opts)
				results[i] = result
			}()
		}

		// Wait for all resolutions to complete
		t.Logf("Starting %d components", numComponents)
		synctest.Wait()
		t.Log("wait finished")

		// Verify all resolutions completed successfully
		for i := range numComponents {
			if results[i] == nil {
				// Try one more time after synctest.Wait
				opts := workerpool.ResolveOptions{
					Component:  fmt.Sprintf("component-%d", i),
					Version:    "v1.0.0",
					KeyFunc:    func() (string, error) { return fmt.Sprintf("ocm-config-%d", i), nil },
					Repository: mockRepo,
				}
				result, err := env.Pool.GetComponentVersion(ctx, opts)
				require.NoError(t, err)
				results[i] = result
			}
			require.NotNil(t, results[i], "component-%d should have a result", i)
			assert.Equal(t, fmt.Sprintf("component-%d", i), results[i].Component.Name)
			assert.Equal(t, "v1.0.0", results[i].Component.Version)
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

		// Use a mock repository that tracks call count
		var callCount atomic.Int32
		mockRepo := &mockRepository{
			GetComponentVersionFn: func(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
				callCount.Add(1)
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
			},
		}

		env := setupTestEnvironment(t, k8sClient, &logger)

		const numConcurrent = 50
		errs := make([]error, numConcurrent)

		// Fire off concurrent requests for the same component - first wave gets ErrResolutionInProgress
		for i := range numConcurrent {
			go func() {
				opts := workerpool.ResolveOptions{
					Component:  "shared-component",
					Version:    "v1.0.0",
					KeyFunc:    func() (string, error) { return "ocm-config-shared", nil },
					Repository: mockRepo,
				}
				_, err := env.Pool.GetComponentVersion(ctx, opts)
				errs[i] = err
			}()
		}

		t.Log("before synctest wait")
		// Wait for resolution to complete
		synctest.Wait()
		t.Log("after synctest wait")

		// Check if it's in cache now
		opts := workerpool.ResolveOptions{
			Component:  "shared-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "ocm-config-shared", nil },
			Repository: mockRepo,
		}
		result, err := env.Pool.GetComponentVersion(ctx, opts)
		require.NoError(t, err, "after wait, should get result from cache")
		require.NotNil(t, result)
		assert.Equal(t, "shared-component", result.Component.Name)

		calls := callCount.Load()
		assert.Equal(t, int32(1), calls, "inProgress tracking should significantly reduce calls (got %d calls for %d concurrent requests)", calls, numConcurrent)
		t.Logf("repository was called %d times for %d concurrent requests (inProgress deduplication working)", calls, numConcurrent)
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

	// Use a slow mock repository to fill the queue
	mockRepo := &mockRepository{
		GetComponentVersionFn: func(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
			time.Sleep(5 * time.Second)
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
		},
	}

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: 1, // Only 1 worker
		QueueSize:   2, // Very small queue
		Logger:      &logger,
		Client:      k8sClient,
		Cache:       cache,
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

	for i := range 10 {
		go func() {
			defer wg.Done()
			opts := workerpool.ResolveOptions{
				Component:  fmt.Sprintf("component-%d", i),
				Version:    "v1.0.0",
				KeyFunc:    func() (string, error) { return fmt.Sprintf("ocm-config-%d", i), nil },
				Repository: mockRepo,
			}
			_, err := wp.GetComponentVersion(ctx, opts)
			if err != nil && !errors.Is(err, resolution.ErrResolutionInProgress) {
				if strings.Contains(err.Error(), "work queue is full") {
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		opts := workerpool.ResolveOptions{
			Component:  "cancel-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "ocm-config", nil },
			Repository: &mockRepository{},
		}

		// Start resolution
		result, err := env.Pool.GetComponentVersion(ctx, opts)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		// Cancel context immediately
		cancel()

		// Wait for workers to stop
		synctest.Wait()

		opts.Component = "cancel-component-new"
		opts.KeyFunc = func() (string, error) { return "ocm-config-new", nil }
		result, err = env.Pool.GetComponentVersion(ctx, opts)
		assert.Nil(t, result)
		assert.EqualError(t, err, "context canceled")
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		versions := []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0"}
		const numConcurrent = 10

		results := make(map[string][]*descriptor.Descriptor)
		for _, v := range versions {
			results[v] = make([]*descriptor.Descriptor, numConcurrent)
		}

		// Create a single shared mock repository to avoid race conditions
		mockRepo := &mockRepository{}

		for _, version := range versions {
			for i := range numConcurrent {
				go func() {
					opts := workerpool.ResolveOptions{
						Component:  "multi-version-component",
						Version:    version,
						KeyFunc:    func() (string, error) { return fmt.Sprintf("ocm-config-%s", version), nil },
						Repository: mockRepo,
					}
					result, _ := env.Pool.GetComponentVersion(ctx, opts)
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
				if results[version][i] == nil {
					// refetch values after sync wait is done for all go routines
					opts := workerpool.ResolveOptions{
						Component:  "multi-version-component",
						Version:    version,
						KeyFunc:    func() (string, error) { return fmt.Sprintf("ocm-config-%s", version), nil },
						Repository: mockRepo,
					}
					result, err := env.Pool.GetComponentVersion(ctx, opts)
					require.NoError(t, err)
					results[version][i] = result
				}
				require.NotNil(t, results[version][i], "version %s, request %d should have a result", version, i)
				assert.Equal(t, "multi-version-component", results[version][i].Component.Name)
				assert.Equal(t, version, results[version][i].Component.Version)
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		// Create a single shared mock repository to avoid race conditions
		mockRepo := &mockRepository{}

		opts1 := workerpool.ResolveOptions{
			Component:  "cache-test",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "ocm-config-1", nil },
			Repository: mockRepo,
		}

		// First resolution with config-1
		_, err := env.Pool.GetComponentVersion(ctx, opts1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result1, err := env.Pool.GetComponentVersion(ctx, opts1)
		require.NoError(t, err)
		require.NotNil(t, result1)

		opts2 := workerpool.ResolveOptions{
			Component:  "cache-test",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "ocm-config-2", nil }, // Different key = different cache entry
			Repository: mockRepo,
		}

		// Second resolution with config-2 (different config = cache miss)
		_, err = env.Pool.GetComponentVersion(ctx, opts2)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		result2, err := env.Pool.GetComponentVersion(ctx, opts2)
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Different configs should produce different cache entries (both should succeed)
		assert.Equal(t, result1.Component.Name, result2.Component.Name)
		assert.Equal(t, result1.Component.Version, result2.Component.Version)
	})
}

// mockRepository is a flexible mock plugin for testing that allows customizing behavior.
type mockRepository struct {
	mu sync.Mutex
	repository.ComponentVersionRepository
	GetComponentVersionFn func(ctx context.Context, component, version string) (*descriptor.Descriptor, error)
}

func (p *mockRepository) GetComponentVersionRepository(
	_ context.Context,
	_ ocmruntime.Typed,
	_ map[string]string,
) (repository.ComponentVersionRepository, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p, nil
}

// GetComponentVersion implements repository.ComponentVersionRepository
func (p *mockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

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

func TestWorkerPoolEventChannelNotifiesRequesters(t *testing.T) {
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

		env := setupTestEnvironment(t, k8sClient, &logger)

		requester1 := workerpool.RequesterInfo{
			NamespacedName: client.ObjectKey{Namespace: "ns1", Name: "component1"},
		}
		requester2 := workerpool.RequesterInfo{
			NamespacedName: client.ObjectKey{Namespace: "ns2", Name: "component2"},
		}
		requester3 := workerpool.RequesterInfo{
			NamespacedName: client.ObjectKey{Namespace: "ns3", Name: "component3"},
		}

		opts1 := workerpool.ResolveOptions{
			Component:  "shared-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "shared-key", nil },
			Repository: &mockRepository{},
			Requester:  requester1,
		}
		opts2 := workerpool.ResolveOptions{
			Component:  "shared-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "shared-key", nil },
			Repository: &mockRepository{},
			Requester:  requester2,
		}
		opts3 := workerpool.ResolveOptions{
			Component:  "shared-component",
			Version:    "v1.0.0",
			KeyFunc:    func() (string, error) { return "shared-key", nil },
			Repository: &mockRepository{},
			Requester:  requester3,
		}

		eventReceived := make(chan []workerpool.RequesterInfo, 1)
		go func() {
			select {
			case requesters := <-env.Pool.EventChannel():
				eventReceived <- requesters
			case <-ctx.Done():
			}
		}()

		_, err := env.Pool.GetComponentVersion(ctx, opts1)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))
		_, err = env.Pool.GetComponentVersion(ctx, opts2)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))
		_, err = env.Pool.GetComponentVersion(ctx, opts3)
		assert.True(t, errors.Is(err, resolution.ErrResolutionInProgress))

		synctest.Wait()

		var requesters []workerpool.RequesterInfo
		select {
		case requesters = <-eventReceived:
		case <-time.After(30 * time.Second):
			t.Fatal("timeout waiting for event notification")
		}

		require.Len(t, requesters, 3, "expected 3 requesters to be notified")

		foundRequesters := make(map[client.ObjectKey]bool)
		for _, r := range requesters {
			foundRequesters[r.NamespacedName] = true
		}

		assert.True(t, foundRequesters[requester1.NamespacedName], "requester1 should be notified")
		assert.True(t, foundRequesters[requester2.NamespacedName], "requester2 should be notified")
		assert.True(t, foundRequesters[requester3.NamespacedName], "requester3 should be notified")
	})
}

func TestWorkerPoolEventChannelClosedOnShutdown(t *testing.T) {
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

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		Logger: &logger,
		Client: k8sClient,
		Cache:  cache,
	})
	t.Cleanup(cancel)

	go func() {
		_ = wp.Start(ctx)
	}()

	channelClosed := make(chan bool, 1)
	go func() {
		for range wp.EventChannel() {
			// drain
		}
		channelClosed <- true
	}()

	cancel()

	select {
	case <-channelClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event channel to close")
	}
}

// testEnvironment holds the test infrastructure for workerpool testing.
type testEnvironment struct {
	Pool *workerpool.WorkerPool
}

// setupTestEnvironment creates a test environment with a worker pool.
func setupTestEnvironment(t *testing.T, k8sClient client.Reader, logger *logr.Logger) *testEnvironment {
	t.Helper()

	cache := expirable.NewLRU[string, *workerpool.Result](0, nil, 0)
	wp := workerpool.NewWorkerPool(workerpool.PoolOptions{
		Logger: logger,
		Client: k8sClient,
		Cache:  cache,
	})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	// Start worker pool in background since Start() blocks for graceful shutdown
	go func() {
		_ = wp.Start(ctx)
	}()

	return &testEnvironment{
		Pool: wp,
	}
}
