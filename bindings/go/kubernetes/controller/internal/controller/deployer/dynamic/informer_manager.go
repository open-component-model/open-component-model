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
	"k8s.io/apimachinery/pkg/util/sets"
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
// - A map of active informer tasks, one per child GVK, each with the parents watching it.
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

	// Protects tasks.
	tasksMu sync.RWMutex
	// Active informer tasks keyed by child GVK. The cache runs one cluster-wide informer per GVK, so a task is shared
	// by every parent watching that GVK, in any namespace.
	tasks map[schema.GroupVersionKind]*watchTask

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

// watchTask holds the informer, its registration handle, and the parents watching it.
type watchTask struct {
	informer     cache.Informer
	registration toolscache.ResourceEventHandlerRegistration
	parents      sets.Set[client.ObjectKey]
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
		tasks:           make(map[schema.GroupVersionKind]*watchTask),
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

// IsStopped reports whether parent no longer watches the GVK of obj. The informer itself may still run for other
// parents.
func (mgr *InformerManager) IsStopped(parent, obj client.Object) bool {
	mgr.tasksMu.RLock()
	defer mgr.tasksMu.RUnlock()

	task, ok := mgr.tasks[obj.GetObjectKind().GroupVersionKind()]

	return !ok || !task.parents.Has(client.ObjectKeyFromObject(parent))
}

// HasSynced reports whether parent watches the GVK of obj and the informer has synced.
func (mgr *InformerManager) HasSynced(parent, obj client.Object) bool {
	mgr.tasksMu.RLock()
	defer mgr.tasksMu.RUnlock()

	task, ok := mgr.tasks[obj.GetObjectKind().GroupVersionKind()]

	return ok && task.parents.Has(client.ObjectKeyFromObject(parent)) &&
		task.informer.HasSynced() && task.registration.HasSynced()
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

// ActiveForParent returns an object per GVK that parent watches.
func (mgr *InformerManager) ActiveForParent(parent client.Object) []client.Object {
	mgr.tasksMu.RLock()
	defer mgr.tasksMu.RUnlock()

	var active []client.Object
	for gvk, task := range mgr.tasks {
		if task.parents.Has(client.ObjectKeyFromObject(parent)) {
			obj := &v1.PartialObjectMetadata{}
			obj.SetGroupVersionKind(gvk)
			active = append(active, obj)
		}
	}

	return active
}

func (mgr *InformerManager) Register(ctx context.Context, parent, obj client.Object) error {
	logger := ctrl.LoggerFrom(ctx)

	mgr.tasksMu.Lock()
	defer mgr.tasksMu.Unlock()

	gvk := obj.GetObjectKind().GroupVersionKind()
	if task, ok := mgr.tasks[gvk]; ok {
		logger.Info("watch is already active", "gvk", gvk)
		task.parents.Insert(client.ObjectKeyFromObject(parent))

		return nil
	}

	// HasSynced reports the sync, so do not block the other workers while holding the lock.
	inf, err := mgr.cache.GetInformer(ctx, obj, cache.BlockUntilSynced(false))
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

	mgr.tasks[gvk] = &watchTask{informer: inf, registration: reg, parents: sets.New(client.ObjectKeyFromObject(parent))}
	activeTasks.WithLabelValues(mgr.metricsLabel).Inc()
	registerTotal.WithLabelValues(mgr.metricsLabel, gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace()).Inc()

	return nil
}

// Unregister removes parent from the watchers of the GVK of obj and stops the informer once no parent watches it.
func (mgr *InformerManager) Unregister(ctx context.Context, parent, obj client.Object) error {
	mgr.tasksMu.Lock()
	defer mgr.tasksMu.Unlock()

	gvk := obj.GetObjectKind().GroupVersionKind()
	task, ok := mgr.tasks[gvk]
	if !ok {
		return nil
	}

	task.parents.Delete(client.ObjectKeyFromObject(parent))
	if task.parents.Len() > 0 {
		ctrl.LoggerFrom(ctx).Info("other parents still watch the GVK, not stopping the informer", "gvk", gvk, "parents", task.parents.Len())

		return nil
	}

	if err := mgr.stopTask(ctx, gvk, task); err != nil {
		return fmt.Errorf("failed to stop task for %s: %w", obj.GetName(), err)
	}

	unregisterTotal.WithLabelValues(mgr.metricsLabel, gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace()).Inc()

	return nil
}

// --- Private Helpers ---

func (mgr *InformerManager) GracefulShutdown(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var err error
	mgr.tasksMu.Lock()
	for gvk, task := range mgr.tasks {
		err = errors.Join(err, mgr.stopTask(ctx, gvk, task))
	}
	mgr.tasksMu.Unlock()

	mgr.queueMu.Lock()
	defer mgr.queueMu.Unlock()
	// dereference the queue to stop processing events
	// the queue lifecycle is managed by the manager, so we don't own its shutdown
	mgr.queue = nil

	close(mgr.register)
	close(mgr.unregister)

	return err
}

// stopTask stops the informer of gvk. The caller must hold tasksMu.
func (mgr *InformerManager) stopTask(ctx context.Context, gvk schema.GroupVersionKind, t *watchTask) error {
	ctrl.LoggerFrom(ctx).Info("Stopping dynamic watch task as no parent watches the GVK anymore", "gvk", gvk)

	if err := t.informer.RemoveEventHandler(t.registration); err != nil {
		return fmt.Errorf("failed to remove event handler for %s: %w", gvk, err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := mgr.cache.RemoveInformer(ctx, obj); err != nil {
		return fmt.Errorf("failed to remove informer for %s: %w", gvk, err)
	}
	delete(mgr.tasks, gvk)
	activeTasks.WithLabelValues(mgr.metricsLabel).Dec()

	return nil
}
