package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	internaldiscovery "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Reconciler reconciles Discovery objects: it resolves the complete
// transitive component graph of the referenced ready Component, evaluates the
// compiled selector/extraction query against it, and atomically publishes the
// resulting payload and conditions into the Discovery status.
//
// The reconciler is synchronous: contrary to BaseReconciler.PluginManagerFor,
// the request-scoped plugin manager is built with the reconcile context so
// cancellation propagates into plugin calls. It does not use the resolution
// worker pool; repository access happens through a request-scoped resolver
// rooted at the referenced Component's repository spec.
type Reconciler struct {
	*ocm.BaseReconciler
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// componentRefIndex keys Discoveries by the Component they discover.
const componentRefIndex = "spec.componentRef.name"

// SetupWithManager sets up the Discovery controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Build index for discoveries that reference a component to make sure that we
	// get notified when a component changes.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Discovery{}, componentRefIndex, func(obj client.Object) []string {
		discovery, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}

		return []string{discovery.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Discovery{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.Component{},
			handler.EnqueueRequestsFromMapFunc(r.mapComponentToDiscoveries),
			builder.WithPredicates(ocm.ComponentInfoChangedPredicate{})).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(10, 100)},
			),
		}).
		Complete(r)
}

// mapComponentToDiscoveries maps a Component to the Discoveries that reference
// it as their graph root, via the same-namespace componentRef index.
func (r *Reconciler) mapComponentToDiscoveries(ctx context.Context, obj client.Object) []reconcile.Request {
	component, ok := obj.(*v1alpha1.Component)
	if !ok {
		return nil
	}

	referencing := &v1alpha1.DiscoveryList{}
	if err := r.List(ctx, referencing,
		client.InNamespace(component.GetNamespace()),
		client.MatchingFields{componentRefIndex: component.GetName()}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list discoveries referencing component as root",
			"component", client.ObjectKeyFromObject(component))

		return nil
	}

	requests := make([]reconcile.Request, 0, len(referencing.Items))
	for i := range referencing.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&referencing.Items[i]),
		})
	}

	return requests
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries/status,verbs=get;update;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	discovery := &v1alpha1.Discovery{}
	if err := r.Get(ctx, req.NamespacedName, discovery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deleting and suspended objects exit without status advancement: the old
	// conditions and payload are retained as-is.
	if !discovery.GetDeletionTimestamp().IsZero() {
		logger.Info("discovery is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}
	if discovery.Spec.Suspend {
		logger.Info("discovery is suspended, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	old := discovery.DeepCopy()
	result, rerr := r.reconcile(ctx, discovery)
	if perr := r.publish(ctx, old, discovery); perr != nil {
		// Status write failures are re-tried.
		return ctrl.Result{}, errors.Join(rerr, perr)
	}

	return result, rerr
}

// reconcile executes the discovery pipeline against discovery and mutates its
// status in memory. Status publication happens separately in Reconcile.
//
// Every failure is terminal: the error is reported on the status and returned as
// a reconcile.TerminalError, so nothing is retried. Recovery comes from a spec
// change or from a watch event on the referenced Component or a configuration
// source, never from a backoff.
//
//nolint:funlen,cyclop // the pipeline is intentionally linear; splitting it would obscure the failure-semantics
func (r *Reconciler) reconcile(ctx context.Context, discovery *v1alpha1.Discovery) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: discovery.GetNamespace(),
		Name:      discovery.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResourceIsNotAvailable, err.Error())
		logger.Info("component is not available", "error", err)

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to get ready component: %w", err))
	}

	info := component.Status.Component
	if info.Component == "" || info.Version == "" || info.RepositorySpec == nil {
		err := fmt.Errorf("component %s has no complete resolved identity and repository spec", component.GetName())
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResourceIsNotAvailable, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), discovery, component)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to get effective config: %w", err))
	}

	// Record the effective config in memory. Reconcile publishes the mutated
	// status whether or not a later step fails, so the used configuration
	// references stay observable without an extra round trip.
	discovery.Status.EffectiveOCMConfig = configs

	if upToDate(discovery, info) {
		logger.V(1).Info("root component digest unchanged, skipping re-discovery",
			"digest", discovery.Status.ObservedComponentDigest)

		// Purely watch-driven: no periodic requeue.
		return ctrl.Result{}, nil
	}

	query, err := internaldiscovery.Compile(ctx, &discovery.Spec)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.SelectorFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	cfg, err := configuration.LoadConfigurations(ctx, r.Client, discovery.GetNamespace(), configs)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to load configurations: %w", err))
	}

	if r.NewPluginManager == nil {
		err := errors.New("no plugin manager factory configured on the reconciler")
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}
	var genericCfg *genericv1.Config
	if cfg != nil {
		genericCfg = cfg.Config
	}
	pm, err := r.NewPluginManager(ctx, genericCfg)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to create plugin manager: %w", err))
	}

	spec := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Decode(bytes.NewReader(info.RepositorySpec.Raw), spec); err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to decode repository spec: %w", err))
	}

	var credentialGraph credentials.Resolver
	if cfg != nil {
		credentialGraph, err = setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        &logger,
		})
		if err != nil {
			status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetRepositoryFailedReason, err.Error())

			return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to create credential graph: %w", err))
		}
	}

	resolver, err := resolvers.NewFromConfig(ctx, genericCfg, ocirepository.Scheme, resolvers.Options{
		RepoProvider:      pm.ComponentVersionRepositoryRegistry,
		CredentialGraph:   credentialGraph,
		ComponentPatterns: []string{info.Component},
	}, spec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to create repository resolver: %w", err))
	}

	graph, err := internaldiscovery.Traverse(ctx,
		internaldiscovery.ComponentKey{Name: info.Component, Version: info.Version},
		resolver)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, discovery, v1alpha1.ResolutionFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("failed to resolve the transitive component graph: %w", err))
	}
	logger.V(1).Info("resolved transitive component graph", "components", len(graph.Descriptors))

	filtered, err := query.Filter(ctx, *graph)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.SelectorFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	payload, err := query.Project(ctx, filtered)
	if err != nil {
		status.MarkAsStalled(r.EventRecorder, discovery, v1alpha1.ExtractFailedReason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	if err := r.setPayload(discovery, payload); err != nil {
		reason := v1alpha1.MarshalFailedReason
		if errors.Is(err, errPayloadTooLarge) {
			reason = v1alpha1.PayloadTooLargeReason
		}
		status.MarkAsStalled(r.EventRecorder, discovery, reason, err.Error())

		return ctrl.Result{}, reconcile.TerminalError(err)
	}

	discovery.Status.ObservedComponentDigest = ""
	if graph.DigestsComplete() {
		discovery.Status.ObservedComponentDigest = digestKey(info.Digest)
	}

	status.MarkReady(r.EventRecorder, discovery, "%s", payloadMessage(payload))
	// Discovery advances the observed generation here because it does not use
	// the shared UpdateBeforePatch defer the other controllers rely on.
	discovery.SetObservedGeneration(discovery.GetGeneration())
	event.New(r.EventRecorder, discovery, discovery.GetVID(), v1alpha1.EventSeverityInfo,
		"Reconciliation finished, no further runs scheduled until next change")

	// Purely watch-driven: no periodic requeue.
	return ctrl.Result{}, nil
}

// maxPayloadBytes etcd cap.
const maxPayloadBytes = 1 << 20

// errPayloadTooLarge happens if the status would be too large to store.
var errPayloadTooLarge = errors.New("status payload exceeds the size limit")

// setPayload swaps the status payload in one in-memory update: the selected
// field is set (possibly to an empty list), the other field is cleared. If the payload
// was too large, return errPayloadTooLarge instead so we can surface that and set
// the object to Ready=False.
func (r *Reconciler) setPayload(discovery *v1alpha1.Discovery, payload *internaldiscovery.Payload) error {
	var size int
	if payload.Extracted != nil {
		records := make([]v1alpha1.ExtractedRecord, 0, len(payload.Extracted))
		for _, record := range payload.Extracted {
			typed := make(v1alpha1.ExtractedRecord, len(record))
			for name, value := range record {
				raw, err := json.Marshal(value)
				if err != nil {
					return fmt.Errorf("failed to marshal extracted field %q: %w", name, err)
				}
				// this is a rough estimate, but is enough for our purposes.
				size += len(name) + len(raw)
				typed[name] = apiextensionsv1.JSON{Raw: raw}
			}
			records = append(records, typed)
		}
		if size > maxPayloadBytes {
			return fmt.Errorf("%w: %d extracted records total %d bytes, limit is %d",
				errPayloadTooLarge, len(records), size, maxPayloadBytes)
		}
		discovery.Status.Extracted = records
		discovery.Status.Components = nil

		return nil
	}

	components := make([]apiextensionsv1.JSON, 0, len(payload.Components))
	for _, raw := range payload.Components {
		size += len(raw)
		components = append(components, apiextensionsv1.JSON{Raw: raw})
	}
	if size > maxPayloadBytes {
		return fmt.Errorf("%w: %d descriptors total %d bytes, limit is %d",
			errPayloadTooLarge, len(components), size, maxPayloadBytes)
	}
	discovery.Status.Components = components
	discovery.Status.Extracted = nil

	return nil
}

// upToDate checks whether the digest of the root component is the same
// as last time we encountered it. If yes, the traverse can be skipped.
//
// A component descriptor digest covers its references' digests, which cover
// theirs, so an unchanged root digest means an unchanged transitive graph. The
// recorded digest is only written when that chain was complete (see
// Graph.DigestsComplete), so an empty value here always forces a full
// re-discovery.
func upToDate(discovery *v1alpha1.Discovery, info v1alpha1.ComponentInfo) bool {
	if discovery.Status.ObservedComponentDigest == "" {
		return false
	}
	if discovery.Status.ObservedGeneration != discovery.GetGeneration() {
		return false
	}
	if discovery.Status.Components == nil && discovery.Status.Extracted == nil {
		return false
	}

	return discovery.Status.ObservedComponentDigest == digestKey(info.Digest)
}

// digestKey renders a digest as a comparable key. The algorithms are part of it
// because the same component yields a different value under a different
// normalisation.
func digestKey(digest *v2.Digest) string {
	if digest == nil || digest.Value == "" {
		return ""
	}

	return fmt.Sprintf("%s:%s:%s", digest.NormalisationAlgorithm, digest.HashAlgorithm, digest.Value)
}

// payloadMessage returns the Ready condition message for a successful
// evaluation.
func payloadMessage(payload *internaldiscovery.Payload) string {
	switch payload.Reason {
	case internaldiscovery.EmptyReasonNoReferencesMatched:
		return "discovery succeeded, but no references matched the reference selector"
	case internaldiscovery.EmptyReasonNoComponentsMatched:
		return "discovery succeeded, but no components matched the component selector"
	default:
		return fmt.Sprintf("discovery succeeded with %d results", resultCount(payload))
	}
}

func resultCount(payload *internaldiscovery.Payload) int {
	if payload.Extracted != nil {
		return len(payload.Extracted)
	}
	return len(payload.Components)
}
