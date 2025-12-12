// Package dynamic provides a dynamic informer manager that can register and unregister
// informers at runtime for arbitrary Kubernetes resources. It leverages controller-runtime
// cache and client-go workqueue to dispatch events dynamically per parent-child relationships.
package dynamic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// DefaultShutdownTimeout is the default duration to wait for informers to shut down gracefully.
const DefaultShutdownTimeout = 30 * time.Second

// Event pairs a parent object with a child resource to register or unregister watches dynamically.
type Event struct {
	// Parent is the owning resource triggering the dynamic watch.
	Parent client.Object
	// Child is the resource type to watch under the parent.
	Child client.Object
}

// InformerManager implements manager.Runnable and allows dynamic registration
// of informers for arbitrary Kubernetes resources at runtime but needs to be started through the controller manager.
var _ manager.Runnable = &InformerManager{}

// InformerManager dynamically manages informer lifecycles for Kubernetes resources.
// It allows controllers to register and unregister watches at runtime based on parent-child relations,
// dispatching events through a workqueue to a supplied EventHandler.
//
// Usage:
//  1. Instantiate via NewInformerManager with desired handler, label selectors, and options.
//  2. Add InformerManager as a manager.Runnable to your controller manager: Source yields a redirection of events to a controller queue.
//  3. Use RegisterChannel to enqueue Event{Parent, Child} when you need to start watching a resource under a parent.
//  4. Use UnregisterChannel to enqueue Event{Parent, Child} to stop watching when cleanup is needed.
//  5. Start your controller manager, lifecycle is managed by the controller manager.
//
// Internally, it maintains:
// - A controller-runtime cache for metadata-only informers.
// - Buffered channels for registration/unregistration requests.
// - A synchronized workqueue bound to reconcile.Request processing.
// - A concurrent map of active informer tasks.
// - Calls to prometheus metrics for operations and active watches.
type InformerManager struct {
	// Number of worker goroutines processing register / unregister events.
	workers int

	// Metadata-only cache hosting dynamic informers for arbitrary GVKs.
	cache cache.Cache

	// Channels for enqueuing dynamic watch Event requests.
	// Sends Parent/Child pairs to register or unregister watches.
	register, unregister chan Event

	// Protects access to the bound workqueue reference.
	queueMu sync.RWMutex
	// The workqueue receiving reconcile.Requests emitted by informer events.
	queue workqueue.TypedRateLimitingInterface[ctrl.Request]

	// Concurrent map of active informer tasks keyed by (parent GVK, child GVK, namespace).
	tasks sync.Map // map[watchTaskKey]*watchTask

	// Handler invoked with Create/Update/Delete event notifications.
	handler handler.EventHandler

	// Label used to group Prometheus metrics for this manager instance.
	metricsLabel string

	// Timeout duration for graceful shutdown of all informers.
	shutdownTimeout time.Duration

	// RESTMapper that is backing the dynamic informers. It is dynamically loaded with resource information
	// and mappings
	mapper meta.RESTMapper
}

// watchTask holds the informer and its registration handle.
type watchTask struct {
	informer     cache.Informer
	registration toolscache.ResourceEventHandlerRegistration
}

// Options configures the InformerManager.
type Options struct {
	Config     *rest.Config    // Kubernetes REST client config
	HTTPClient *http.Client    // HTTP client for cache retrieval
	RESTMapper meta.RESTMapper // RESTMapper for GVK discovery (optional)

	Handler handler.EventHandler // event handler for dynamic events

	DefaultLabelSelector labels.Selector // default label filter for informers

	Workers int // number of worker goroutines for register/unregister processing

	RegisterChannelBufferSize   int // buffer size for RegisterChannel
	UnregisterChannelBufferSize int // buffer size for UnregisterChannel

	ShutdownTimeout time.Duration // graceful shutdown timeout

	MetricsLabel string // label for metrics grouping
}

// NewInformerManager constructs an InformerManager with the given options.
// It creates a controller-runtime cache for metadata-only informers.
func NewInformerManager(opts *Options) (*InformerManager, error) {
	mapper := opts.RESTMapper
	if mapper == nil {
		var err error
		if mapper, err = apiutil.NewDynamicRESTMapper(opts.Config, opts.HTTPClient); err != nil {
			return nil, fmt.Errorf("failed to create REST mapper: %w", err)
		}
	}

	// Here we store the dynamic data in a cache.
	// Note that we do not pass a scheme here because we only work with partial metadata
	metadataCache, err := cache.New(opts.Config, cache.Options{
		HTTPClient:                   opts.HTTPClient,
		Mapper:                       mapper,
		ReaderFailOnMissingInformer:  true,
		DefaultLabelSelector:         opts.DefaultLabelSelector,
		DefaultTransform:             TransformPartialObjectMetadata,
		DefaultUnsafeDisableDeepCopy: ptr.To(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	workers := 1 // default to 1 worker if not specified
	if opts.Workers > 0 {
		workers = opts.Workers
	}

	shutdownTimeout := opts.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = DefaultShutdownTimeout
	}

	mgr := &InformerManager{
		cache:           metadataCache,
		register:        make(chan Event, opts.RegisterChannelBufferSize),
		unregister:      make(chan Event, opts.UnregisterChannelBufferSize),
		handler:         opts.Handler,
		workers:         workers,
		metricsLabel:    opts.MetricsLabel,
		shutdownTimeout: shutdownTimeout,
		mapper:          mapper,
	}

	return mgr, nil
}

func (mgr *InformerManager) Source() source.TypedSource[reconcile.Request] {
	return source.Func(func(_ context.Context, w workqueue.TypedRateLimitingInterface[ctrl.Request]) error {
		// this dynamically binds the given queue to the informer manager
		// this means that from this point on, the queue will receive events for all registered watches
		return mgr.SetQueue(w)
	})
}

// RESTMapper returns a meta.RESTMapper with its discovery data filled by all informers
func (mgr *InformerManager) RESTMapper() meta.RESTMapper {
	return mgr.mapper
}

func (mgr *InformerManager) NeedLeaderElection() bool {
	// this manager does need leader election, as it is designed to run in a single instance
	// this is to ensure that the dynamic informers are not started multiple times across different controller instances
	return true
}

func (mgr *InformerManager) Start(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("name", mgr.metricsLabel)
	logger.Info("Starting Dynamic Informer Manager")

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		// start the cache that holds our dynamic informer states.
		// note that we do not need to wait for a sync here because we only register dynamic informers
		// and the cache will not have any initial data.
		return mgr.cache.Start(ctx)
	})

	for range mgr.workers {
		eg.Go(func() error {
			return mgr.work(ctx)
		})
	}

	// Shutdown logic
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	logger.Info("Shutting down dynamic informer manager", "timeout", mgr.shutdownTimeout)

	//nolint:contextcheck // we are using context.Background() here because after the shutdown we don't have the
	// origin context anymore.
	if err := mgr.GracefulShutdown(context.Background(), mgr.shutdownTimeout); err != nil {
		return fmt.Errorf("failed to gracefully shutdown informer manager: %w", err)
	}

	return nil
}

// --- Public State Helpers ---

func (mgr *InformerManager) IsStopped(parent, obj client.Object) bool {
	_, task, ok := mgr.getTask(parent, obj)

	return ok && task.informer.IsStopped()
}

func (mgr *InformerManager) HasSynced(parent, obj client.Object) bool {
	_, task, ok := mgr.getTask(parent, obj)

	return ok && task.informer.HasSynced() && task.registration.HasSynced()
}

// --- Register / Unregister ---

func (mgr *InformerManager) work(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case obj := <-mgr.register:
			timer := prometheus.NewTimer(workerOperationDuration.WithLabelValues(mgr.metricsLabel, "register"))
			err := mgr.Register(ctx, obj.Parent, obj.Child)
			timer.ObserveDuration()
			if err != nil {
				logger.Error(err, "register failed", "Parent", obj, "Child", obj.Child)
			}
		case obj := <-mgr.unregister:
			timer := prometheus.NewTimer(workerOperationDuration.WithLabelValues(mgr.metricsLabel, "unregister"))
			err := mgr.Unregister(ctx, obj.Parent, obj.Child)
			timer.ObserveDuration()
			if err != nil {
				logger.Error(err, "unregister failed", "Parent", obj, "Child", obj.Child)
			}
		}
	}
}

func (mgr *InformerManager) RegisterChannel() chan Event {
	return mgr.register
}

func (mgr *InformerManager) UnregisterChannel() chan Event {
	return mgr.unregister
}

func (mgr *InformerManager) SetQueue(queue workqueue.TypedRateLimitingInterface[ctrl.Request]) error {
	mgr.queueMu.Lock()
	defer mgr.queueMu.Unlock()

	if mgr.queue != nil {
		return fmt.Errorf("another queue is already registered")
	}

	mgr.queue = queue

	return nil
}

func (mgr *InformerManager) ActiveForParent(parent client.Object) []client.Object {
	var active []client.Object
	mgr.tasks.Range(func(k, _ any) bool {
		key := k.(watchTaskKey) //nolint:forcetypeassert // we know the type is a watchTaskKey
		if key.parent == parent.GetObjectKind().GroupVersionKind() {
			obj := &v1.PartialObjectMetadata{}
			obj.SetGroupVersionKind(key.gvk)
			obj.SetNamespace(key.namespace)
			active = append(active, obj)
		}

		return true // continue iterating
	})

	return active
}

func (mgr *InformerManager) Register(ctx context.Context, parent, obj client.Object) error {
	logger := ctrl.LoggerFrom(ctx)

	key := key(parent, obj)
	if _, ok := mgr.tasks.Load(key); ok {
		logger.Info("watch is already active", "gvk", key.gvk, "namespace", key.namespace)

		return nil // already registered
	}

	inf, err := mgr.cache.GetInformer(ctx, obj)
	if err != nil {
		return fmt.Errorf("failed to get informer for %s: %w", obj.GetName(), err)
	}

	withQueue := func(f func(queue workqueue.TypedRateLimitingInterface[ctrl.Request])) {
		mgr.queueMu.RLock()
		defer mgr.queueMu.RUnlock()
		if mgr.queue == nil {
			logger.Error(fmt.Errorf("queue is not set"), "cannot process event", "object", obj)

			return
		}
		f(mgr.queue)
	}

	eventHandler := toolscache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(o any, isInit bool) {
			withQueue(func(queue workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				eventCount.WithLabelValues(mgr.metricsLabel, "add").Inc()
				mgr.handler.Create(ctx, event.TypedCreateEvent[client.Object]{
					Object:          o.(client.Object), //nolint:forcetypeassert // we know the type is client.Object
					IsInInitialList: isInit,
				}, queue)
			})
		},
		UpdateFunc: func(oldObject, newObject any) {
			withQueue(func(queue workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				eventCount.WithLabelValues(mgr.metricsLabel, "update").Inc()
				mgr.handler.Update(ctx, event.TypedUpdateEvent[client.Object]{
					ObjectNew: newObject.(client.Object), //nolint:forcetypeassert // we know the type is client.Object
					ObjectOld: oldObject.(client.Object), //nolint:forcetypeassert // we know the type is client.Object
				}, queue)
			})
		},
		DeleteFunc: func(o any) {
			withQueue(func(queue workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				eventCount.WithLabelValues(mgr.metricsLabel, "delete").Inc()
				mgr.handler.Delete(ctx, event.TypedDeleteEvent[client.Object]{
					Object: o.(client.Object), //nolint:forcetypeassert // we know the type is client.Object
				}, queue)
			})
		},
	}

	reg, err := inf.AddEventHandlerWithOptions(eventHandler, toolscache.HandlerOptions{Logger: &logger})
	if err != nil {
		return fmt.Errorf("failed to add event handler for %s: %w", obj.GetName(), err)
	}

	t := &watchTask{informer: inf, registration: reg}
	mgr.tasks.Store(key, t)
	activeTasks.WithLabelValues(mgr.metricsLabel).Inc()
	registerTotal.WithLabelValues(mgr.metricsLabel, key.gvk.Group, key.gvk.Version, key.gvk.Kind, key.namespace).Inc()

	return nil
}

func (mgr *InformerManager) Unregister(ctx context.Context, parent, obj client.Object) error {
	key, task, ok := mgr.getTask(parent, obj)
	if !ok {
		return nil
	}

	if err := mgr.stopTask(ctx, key, task); err != nil {
		return fmt.Errorf("failed to stop task for %s: %w", obj.GetName(), err)
	}

	unregisterTotal.WithLabelValues(mgr.metricsLabel, key.gvk.Group, key.gvk.Version, key.gvk.Kind, key.namespace).Inc()

	return nil
}

// --- Private Helpers ---

// watchTaskKey uniquely identifies a dynamic watch by parent GVK, resource GVK, and namespace.
type watchTaskKey struct {
	// parent is the GVK of the parent object that owns this watch.
	parent schema.GroupVersionKind
	// gvk is the GVK of the child object being watched.
	gvk schema.GroupVersionKind
	// namespace is an optional namespace for the child object for a namespaced watch.
	namespace string
}

// key generates a watchTaskKey for the given parent and child object.
func key(parent, obj client.Object) watchTaskKey {
	return watchTaskKey{
		parent:    parent.GetObjectKind().GroupVersionKind(),
		gvk:       obj.GetObjectKind().GroupVersionKind(),
		namespace: obj.GetNamespace(),
	}
}

func (mgr *InformerManager) getTask(parent, obj client.Object) (watchTaskKey, *watchTask, bool) {
	k := key(parent, obj)
	t, ok := mgr.tasks.Load(k)
	if !ok {
		return watchTaskKey{}, nil, false
	}

	task, ok := t.(*watchTask)
	if !ok {
		return watchTaskKey{}, nil, false
	}

	return k, task, true
}

func (mgr *InformerManager) GracefulShutdown(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var err error
	mgr.tasks.Range(func(k, v any) bool {
		err = errors.Join(err, mgr.stopTask(ctx, k.(watchTaskKey), v.(*watchTask))) //nolint:forcetypeassert // we know the type

		return true
	})

	mgr.queueMu.Lock()
	defer mgr.queueMu.Unlock()
	// dereference the queue to stop processing events
	// the queue lifecycle is managed by the manager, so we don't own its shutdown
	mgr.queue = nil

	close(mgr.register)
	close(mgr.unregister)

	return err
}

func (mgr *InformerManager) stopTask(ctx context.Context, k watchTaskKey, t *watchTask) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("gvk", k.gvk, "namespace", k.namespace)

	isLastWatch := true
	mgr.tasks.Range(func(ek, _ any) bool {
		existing := ek.(watchTaskKey) //nolint:forcetypeassert // we know the type is watchTaskKey
		if existing.gvk == k.gvk && existing.namespace == k.namespace && existing.parent != k.parent {
			isLastWatch = false
		}

		return true // continue iterating
	})
	if !isLastWatch {
		logger.Info("Found another active watch task for the same GVK and namespace but differing Parent, not stopping yet")

		return nil
	}

	logger.Info("Stopping dynamic watch task as this was the last watch for the GVK and namespace")

	if err := t.informer.RemoveEventHandler(t.registration); err != nil {
		return fmt.Errorf("failed to remove event handler for %s: %w", k.gvk, err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(k.gvk)
	if err := mgr.cache.RemoveInformer(ctx, obj); err != nil {
		return fmt.Errorf("failed to remove informer for %s: %w", k.gvk, err)
	}
	mgr.tasks.Delete(k)
	activeTasks.WithLabelValues(mgr.metricsLabel).Dec()

	return nil
}
