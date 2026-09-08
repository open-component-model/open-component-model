package discovery

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

const (
	// ComponentRefIndex indexes Discoveries by their spec.componentRef.name
	// for same-namespace lookups. It is shared with the Component controller,
	// which uses it for its deletion guard.
	ComponentRefIndex = "spec.componentRef.name"

	// ocmConfigIndex indexes Discoveries by their explicit spec.ocmConfig
	// entries, normalized as kind/namespace/name. Entries are indexed even
	// when the referenced source does not exist, so that its creation
	// triggers a reconcile.
	ocmConfigIndex = "spec.ocmConfig"

	// effectiveOCMConfigIndex indexes Discoveries by their
	// status.effectiveOCMConfig entries (Secret/ConfigMap references),
	// normalized as kind/namespace/name.
	effectiveOCMConfigIndex = "status.effectiveOCMConfig"
)

// registeredIndexers guards field-index registration against double
// registration on the same manager cache, shared between the Discovery and
// Component controllers as well as standalone envtest suites.
var registeredIndexers sync.Map

// indexed caches the fact that the index was registered for the given indexer.
func indexed(indexer client.FieldIndexer) bool {
	_, ok := registeredIndexers.Load(indexer)
	return ok
}

func markIndexed(indexer client.FieldIndexer) {
	registeredIndexers.Store(indexer, struct{}{})
}

// EnsureComponentRefIndex registers the Discovery component-reference index on
// the manager once. It is safe to call from multiple controllers and suites
// sharing the same manager.
func EnsureComponentRefIndex(ctx context.Context, mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()
	if indexed(indexer) {
		return nil
	}
	if err := indexer.IndexField(ctx, &v1alpha1.Discovery{}, ComponentRefIndex, func(obj client.Object) []string {
		d, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}
		return []string{d.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting discovery component reference index: %w", err)
	}
	markIndexed(indexer)
	return nil
}

// configKey normalizes a configuration reference as kind/namespace/name.
func configKey(kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
}

// registerConfigIndexes registers the explicit and effective configuration
// reference indexes on the manager.
func registerConfigIndexes(ctx context.Context, mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()
	if err := indexer.IndexField(ctx, &v1alpha1.Discovery{}, ocmConfigIndex, func(obj client.Object) []string {
		d, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}
		keys := make([]string, 0, len(d.Spec.OCMConfig))
		for _, cfg := range d.Spec.OCMConfig {
			namespace := cfg.Namespace
			if namespace == "" {
				namespace = d.GetNamespace()
			}
			keys = append(keys, configKey(cfg.Kind, namespace, cfg.Name))
		}
		return keys
	}); err != nil {
		return fmt.Errorf("failed setting discovery ocm config index: %w", err)
	}
	if err := indexer.IndexField(ctx, &v1alpha1.Discovery{}, effectiveOCMConfigIndex, func(obj client.Object) []string {
		d, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}
		keys := make([]string, 0, len(d.Status.EffectiveOCMConfig))
		for _, cfg := range d.Status.EffectiveOCMConfig {
			namespace := cfg.Namespace
			if namespace == "" {
				namespace = d.GetNamespace()
			}
			keys = append(keys, configKey(cfg.Kind, namespace, cfg.Name))
		}
		return keys
	}); err != nil {
		return fmt.Errorf("failed setting discovery effective ocm config index: %w", err)
	}
	return nil
}

// SetupWithManager sets up the Discovery controller with the Manager:
// the Discovery component-reference and configuration indexes, the watches on
// the root Component, the referenced configuration sources, and the normal
// backoff rate limiter.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := ValidateSafetyInterval(r.SafetyInterval); err != nil {
		return err
	}
	if err := EnsureComponentRefIndex(ctx, mgr); err != nil {
		return err
	}
	if err := registerConfigIndexes(ctx, mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Discovery{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.Component{},
			handler.EnqueueRequestsFromMapFunc(r.mapComponentToDiscoveries),
			builder.WithPredicates(ocm.ComponentInfoChangedPredicate{})).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigSourceToDiscoveries("Secret")),
			builder.WithPredicates(dataChangedPredicate{})).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigSourceToDiscoveries("ConfigMap")),
			builder.WithPredicates(dataChangedPredicate{})).
		Watches(
			&v1alpha1.Repository{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigProviderToDiscoveries(v1alpha1.KindRepository)),
			builder.WithPredicates(effectiveConfigChangedPredicate{})).
		Watches(
			&v1alpha1.Resource{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigProviderToDiscoveries(v1alpha1.KindResource)),
			builder.WithPredicates(effectiveConfigChangedPredicate{})).
		Watches(
			&v1alpha1.Replication{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigProviderToDiscoveries(v1alpha1.KindReplication)),
			builder.WithPredicates(effectiveConfigChangedPredicate{})).
		Watches(
			&v1alpha1.Discovery{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigProviderToDiscoveries(v1alpha1.KindDiscovery)),
			builder.WithPredicates(effectiveConfigChangedPredicate{})).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(10, 100)},
			),
		}).
		Complete(r)
}

// mapComponentToDiscoveries maps a Component to the Discoveries that either
// reference it as their graph root (same namespace via the componentRef index)
// or reference it as an explicit configuration provider.
func (r *Reconciler) mapComponentToDiscoveries(ctx context.Context, obj client.Object) []reconcile.Request {
	component, ok := obj.(*v1alpha1.Component)
	if !ok {
		return nil
	}
	requests := map[types.NamespacedName]struct{}{}

	referencing := &v1alpha1.DiscoveryList{}
	if err := r.List(ctx, referencing,
		client.InNamespace(component.GetNamespace()),
		client.MatchingFields{ComponentRefIndex: component.GetName()}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list discoveries referencing component as root",
			"component", client.ObjectKeyFromObject(component))
	} else {
		for _, d := range referencing.Items {
			requests[client.ObjectKeyFromObject(&d)] = struct{}{}
		}
	}

	for _, req := range r.discoveriesByConfigKey(ctx, ocmConfigIndex, configKey(v1alpha1.KindComponent, component.GetNamespace(), component.GetName())) {
		requests[req] = struct{}{}
	}

	return namespacedNames(requests)
}

// mapConfigSourceToDiscoveries maps a Secret or ConfigMap to the Discoveries
// that reference it explicitly (spec.ocmConfig) or effectively
// (status.effectiveOCMConfig). The kind is passed explicitly because objects
// delivered by the informer cache do not reliably carry a TypeMeta.
func (r *Reconciler) mapConfigSourceToDiscoveries(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		requests := map[types.NamespacedName]struct{}{}
		key := configKey(kind, obj.GetNamespace(), obj.GetName())
		for _, index := range []string{ocmConfigIndex, effectiveOCMConfigIndex} {
			for _, req := range r.discoveriesByConfigKey(ctx, index, key) {
				requests[req] = struct{}{}
			}
		}
		return namespacedNames(requests)
	}
}

// mapConfigProviderToDiscoveries maps an OCM config provider object
// (Repository, Component, Resource, Replication, Discovery) to the Discoveries
// that reference it explicitly in spec.ocmConfig. The kind is passed
// explicitly because objects delivered by the informer cache do not reliably
// carry a TypeMeta.
func (r *Reconciler) mapConfigProviderToDiscoveries(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		requests := map[types.NamespacedName]struct{}{}
		for _, req := range r.discoveriesByConfigKey(ctx, ocmConfigIndex, configKey(kind, obj.GetNamespace(), obj.GetName())) {
			requests[req] = struct{}{}
		}
		return namespacedNames(requests)
	}
}

// discoveriesByConfigKey lists Discoveries carrying the given normalized
// configuration-reference index key. Failures are logged and yield no
// requests; the safety interval covers the missed lookup.
func (r *Reconciler) discoveriesByConfigKey(ctx context.Context, index, key string) []types.NamespacedName {
	list := &v1alpha1.DiscoveryList{}
	if err := r.List(ctx, list, client.MatchingFields{index: key}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list discoveries by configuration reference", "index", index, "key", key)
		return nil
	}
	requests := make([]types.NamespacedName, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, client.ObjectKeyFromObject(&list.Items[i]))
	}
	return requests
}

func namespacedNames(set map[types.NamespacedName]struct{}) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(set))
	for name := range set {
		requests = append(requests, reconcile.Request{NamespacedName: name})
	}
	return requests
}

// dataChangedPredicate lets Secret/ConfigMap events pass only when the payload
// changed: creation and deletion always pass; updates pass when data,
// binaryData, or (for Secrets) type changed. Pure metadata updates are
// suppressed.
type dataChangedPredicate struct {
	predicate.Funcs
}

func (dataChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}
	switch old := e.ObjectOld.(type) {
	case *corev1.Secret:
		now, ok := e.ObjectNew.(*corev1.Secret)
		if !ok {
			return false
		}
		return !reflect.DeepEqual(old.Data, now.Data) ||
			old.Type != now.Type
	case *corev1.ConfigMap:
		now, ok := e.ObjectNew.(*corev1.ConfigMap)
		if !ok {
			return false
		}
		return !reflect.DeepEqual(old.Data, now.Data) ||
			!reflect.DeepEqual(old.BinaryData, now.BinaryData)
	default:
		return false
	}
}

// effectiveConfigChangedPredicate lets events of OCM config provider objects
// pass when their effective configuration changed: creation and deletion
// always pass; updates pass only when status.effectiveOCMConfig changed.
type effectiveConfigChangedPredicate struct {
	predicate.Funcs
}

func (effectiveConfigChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}
	old, ok := e.ObjectOld.(v1alpha1.ConfigRefProvider)
	if !ok {
		return false
	}
	now, ok := e.ObjectNew.(v1alpha1.ConfigRefProvider)
	if !ok {
		return false
	}
	return !equality.Semantic.DeepEqual(old.GetEffectiveOCMConfig(), now.GetEffectiveOCMConfig())
}
