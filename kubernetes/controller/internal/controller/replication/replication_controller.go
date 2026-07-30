package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferspec "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/kubernetes/controller/pkg/configuration"
)

const (
	componentRefIndex  = "spec.componentRef.name"
	targetRepoRefIndex = "spec.targetRepositoryRef.name"
)

type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver provides repository resolution and caching for the transfer source.
	Resolver *resolution.Resolver

	// PluginManager manages plugins required for transfer operations.
	PluginManager *manager.PluginManager

	// RepositoryScheme decodes repository specs into their concrete types for
	// the transfer library. Must be the same scheme the repository provider is
	// built with, so Replication accepts exactly the spec types Component
	// resolution accepts.
	RepositoryScheme *runtime.Scheme
}

var _ ocm.Reconciler = (*Reconciler)(nil)

func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, concurrency int) error {
	// Index replications by the component they reference so component changes can be mapped back.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Replication{}, componentRefIndex, func(obj client.Object) []string {
		replication, ok := obj.(*v1alpha1.Replication)
		if !ok {
			return nil
		}

		return []string{replication.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting componentRef index: %w", err)
	}

	// Index replications by the target repository so target repository changes can be mapped back.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Replication{}, targetRepoRefIndex, func(obj client.Object) []string {
		replication, ok := obj.(*v1alpha1.Replication)
		if !ok {
			return nil
		}

		return []string{replication.Spec.TargetRepositoryRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting targetRepositoryRef index: %w", err)
	}

	eventSource := workerpool.NewEventSource(r.Resolver.WorkerPool())

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Replication{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(eventSource).
		Watches(
			&v1alpha1.Component{},
			handler.EnqueueRequestsFromMapFunc(r.replicationsForIndex(componentRefIndex)),
			builder.WithPredicates(ComponentInfoChangedPredicate{}),
		).
		Watches(
			&v1alpha1.Repository{},
			handler.EnqueueRequestsFromMapFunc(r.replicationsForIndex(targetRepoRefIndex)),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: concurrency,
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 5*time.Minute),
				&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(10, 100)},
			),
		}).
		Complete(r)
}

// replicationsForIndex returns a mapping function that enqueues every Replication
// whose given field index matches the changed object's name.
func (r *Reconciler) replicationsForIndex(index string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		list := &v1alpha1.ReplicationList{}
		if err := r.List(ctx, list, client.MatchingFields{index: obj.GetName()}); err != nil {
			return nil
		}

		requests := make([]reconcile.Request, 0, len(list.Items))
		for _, replication := range list.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: replication.GetNamespace(),
					Name:      replication.GetName(),
				},
			})
		}

		return requests
	}
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=replications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=replications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=replications/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	replication := &v1alpha1.Replication{}
	if err := r.Get(ctx, req.NamespacedName, replication); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	old := replication.DeepCopy()
	defer func(ctx context.Context) {
		status.UpdateBeforePatch(replication, r.EventRecorder, 0, err)
		if !equality.Semantic.DeepEqual(replication.Status, old.Status) {
			err = errors.Join(err, r.GetClient().Status().Patch(ctx, replication, client.MergeFrom(old)))
		}
	}(ctx)

	// Deletion is handled before suspend so a suspended Replication can still be drained.
	if !replication.GetDeletionTimestamp().IsZero() {
		// TODO(skarlso): per ADR 0020 deletion semantics should cancel in-flight transfer, right now
		// since the transfer is sequential, cancel is when the reconcile context is cancelled.
		// https://github.com/open-component-model/ocm-project/issues/1148
		if updated := controllerutil.RemoveFinalizer(replication, v1alpha1.ReplicationFinalizer); updated {
			if err := r.Update(ctx, replication); err != nil {
				status.MarkNotReady(r.EventRecorder, replication, v1alpha1.DeletionFailedReason, err.Error())

				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}

		return ctrl.Result{}, nil
	}

	if replication.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	if updated := controllerutil.AddFinalizer(replication, v1alpha1.ReplicationFinalizer); updated {
		if err := r.Update(ctx, replication); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	return r.reconcile(ctx, replication)
}

// reconcile runs Phase 1 (plan): it gates on the source Component being ready with a digest,
// decides whether a transfer is needed, and executes the transfer graph (Phase 2).
func (r *Reconciler) reconcile(ctx context.Context, replication *v1alpha1.Replication) (_ ctrl.Result, retErr error) {
	logger := log.FromContext(ctx)

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: replication.GetNamespace(),
		Name:      replication.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ResourceIsNotAvailable, err.Error())

		if errIsUnavailable(err) {
			logger.Info("source component is not available, waiting for component event", "error", err)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	// The target repository must exist and be ready before a transfer can run.
	targetRepository, err := util.GetReadyObject[v1alpha1.Repository, *v1alpha1.Repository](ctx, r.Client, client.ObjectKey{
		Namespace: replication.GetNamespace(),
		Name:      replication.Spec.TargetRepositoryRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		if errIsUnavailable(err) {
			logger.Info("target repository is not available, waiting for repository event", "error", err)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready target repository: %w", err)
	}

	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), replication, component)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), replication, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to configure context: %w", err)
	}

	// The target repository's credentials might live on its own config and are not gathered by
	// GetEffectiveConfig. We add them here explicitly so the credential graph can authenticate
	// if additional ocmconfigs are defined by the targetRepositroy.
	configs = append(configs, targetRepository.GetEffectiveOCMConfig()...)

	// Persist the effective config immediately so the deferred patch keeps it even if a later step fails.
	if !equality.Semantic.DeepEqual(replication.Status.EffectiveOCMConfig, configs) {
		replication.Status.EffectiveOCMConfig = configs

		return ctrl.Result{}, fmt.Errorf("effective ocm config changed")
	}

	sourceDigest := component.Status.Component.Digest.Value
	replication.Status.Component = component.Status.Component.DeepCopy()

	if sourceDigest == replication.Status.LastTransferredDigest {
		status.SetCondition(replication, metav1.Condition{
			Type:    v1alpha1.TransferInProgressCondition,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TransferCompleteReason,
			Message: "source digest already transferred",
		})
		status.MarkReady(r.EventRecorder, replication, "Successfully transferred component version %s", component.Status.Component.Version)

		return ctrl.Result{}, nil
	}

	// if there was an error, clear the in progress condition.
	defer func() {
		if retErr != nil {
			status.SetCondition(replication, metav1.Condition{
				Type:    v1alpha1.TransferInProgressCondition,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.ReplicationFailedReason,
				Message: fmt.Sprintf("transfer attempt aborted because of error: %s", retErr.Error()),
			})
		}
	}()

	sourceSpec, err := convertToTyped(r.RepositoryScheme, component.Status.Component.RepositorySpec.Raw)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to decode source repository spec: %w", err)
	}

	targetSpec, err := convertToTyped(r.RepositoryScheme, targetRepository.Spec.RepositorySpec.Raw)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to decode target repository spec: %w", err)
	}

	cfg, err := configuration.LoadConfigurations(ctx, r.Client, replication.GetNamespace(), configs)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetConfigurationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to load configurations: %w", err)
	}

	cacheBackedRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:  sourceSpec,
		Configuration:   cfg,
		SigningRegistry: r.PluginManager.SigningRegistry,
		RequesterFunc: func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: replication.GetNamespace(),
					Name:      replication.GetName(),
				},
			}
		},
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	// Reuse the configurations already loaded above to look up transfer settings; a nil cfg is valid.
	var transferCfg *transferspec.Config
	if cfg != nil {
		transferCfg, err = transferspec.LookupConfig(cfg.Config)
		if err != nil {
			status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetConfigurationFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to load transfer config: %w", err)
		}
	}

	// Descriptor fetches for discovery go through the resolution service. Uncached component version
	// aborts the walk with `ErrResolutionInProgress` and the event from the resolution service will retrigger
	// this object once done fetching.
	//
	// The process takes turns to complete: resolved descriptors are cache hits, each
	// pass enqueues the next component version of the graph until all component versions are in the cache and
	// accounted for.
	tgd, err := transfer.BuildGraphDefinition(ctx, transferCfg, transfer.Mapping{
		Components: []transfer.ComponentID{{
			Component: component.Status.Component.Component,
			Version:   component.Status.Component.Version,
		}},
		Target:   targetSpec,
		Resolver: transfer.NewRepositoryResolver(cacheBackedRepo, sourceSpec),
	})
	switch {
	case errors.Is(err, workerpool.ErrResolutionInProgress):
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ResolutionInProgress, err.Error())
		logger.Info("transfer graph discovery in progress, waiting for resolution event",
			"component", component.Status.Component.Component,
			"version", component.Status.Component.Version)

		return ctrl.Result{}, nil
	case err != nil:
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to build transfer graph definition: %w", err)
	}

	content, err := json.Marshal(tgd)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to marshal transfer graph definition: %w", err)
	}
	logger.V(1).Info("the entire transfer graph serialized", "graph", string(content))

	status.SetCondition(replication, metav1.Condition{
		Type:    v1alpha1.TransferInProgressCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TransferInProgressReason,
		Message: fmt.Sprintf("transferring component version %s", component.Status.Component.Version),
	})

	if err := r.transfer(ctx, logger, replication, cfg, tgd, component, sourceDigest); err != nil {
		return ctrl.Result{}, err
	}

	status.MarkReady(r.EventRecorder, replication, "Successfully transferred component version %s", component.Status.Component.Version)

	return ctrl.Result{}, nil
}

func (r *Reconciler) transfer(ctx context.Context, logger logr.Logger, replication *v1alpha1.Replication, cfg *configuration.Configuration, tgd *transformv1alpha1.TransformationGraphDefinition, component *v1alpha1.Component, sourceDigest string) error {
	var (
		credGraph credentials.Resolver
		err       error
	)
	if cfg != nil {
		credGraph, err = setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: r.PluginManager,
			Logger:        &logger,
		})
		if err != nil {
			status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetConfigurationFailedReason, err.Error())

			return fmt.Errorf("failed to create credential graph: %w", err)
		}
	}

	events := make(chan graphRuntime.ProgressEvent)

	transferGraph, err := transfer.NewDefaultBuilder(
		r.PluginManager.ComponentVersionRepositoryRegistry,
		r.PluginManager.ResourcePluginRegistry,
		credGraph,
	).WithEvents(events).BuildAndCheck(tgd)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, err.Error())

		return fmt.Errorf("failed to build transformation graph: %w", err)
	}

	var failed []v1alpha1.TransferEvent

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range events {
			if e.Err != nil {
				failed = append(failed, toFailedTransferEvent(e))
			}
		}
	}()

	logger.Info("executing transfer",
		"component", component.Status.Component.Component,
		"version", component.Status.Component.Version,
		"sourceDigest", sourceDigest,
		"transformations", len(tgd.Transformations))

	processErr := transferGraph.Process(ctx)
	wg.Wait()

	if processErr != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, processErr.Error())
		replication.Status.LastFailedTransferEvents = failed

		return fmt.Errorf("failed to process transformation graph: %w", processErr)
	}

	replication.Status.LastTransferredVersion = component.Status.Component.Version
	replication.Status.LastTransferredDigest = sourceDigest
	replication.Status.LastFailedTransferEvents = nil
	status.SetCondition(replication, metav1.Condition{
		Type:    v1alpha1.TransferInProgressCondition,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.TransferCompleteReason,
		Message: fmt.Sprintf("transferred component version %s", component.Status.Component.Version),
	})
	return nil
}

// toFailedTransferEvent convert ProgressEvents to minimal, serializable objects.
func toFailedTransferEvent(e graphRuntime.ProgressEvent) v1alpha1.TransferEvent {
	t := e.Transformation

	return v1alpha1.TransferEvent{
		ID:    t.ID,
		Name:  fmt.Sprintf("%s [%s]", t.ID, t.Type.Name),
		Error: e.Err.Error(),
	}
}

// errIsUnavailable checks if during a Get of an object we detected either not ready error
// or a deletion request.
func errIsUnavailable(err error) bool {
	var notReadyErr util.NotReadyError
	var deletionErr util.DeletionError

	return errors.As(err, &notReadyErr) || errors.As(err, &deletionErr)
}

// convertToTyped converts a runtime.Raw repository spec to a concrete spec.
func convertToTyped(scheme *runtime.Scheme, data []byte) (runtime.Typed, error) {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	obj, err := scheme.NewObject(raw.Type)
	if err != nil {
		return nil, fmt.Errorf("unsupported repository spec type %q for transfer: %w", raw.Type, err)
	}

	if err := scheme.Convert(raw, obj); err != nil {
		return nil, fmt.Errorf("failed to convert repository spec to concrete type: %w", err)
	}

	return obj, nil
}
