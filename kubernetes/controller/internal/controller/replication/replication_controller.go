package replication

import (
	"context"
	"errors"
	"fmt"
	"time"

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

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

const (
	componentRefIndex  = "spec.componentRef.name"
	targetRepoRefIndex = "spec.targetRepositoryRef.name"
)

// errTransferNotImplemented signals that the asynchronous transfer execution
// (Phase 2) is not wired yet. The reconcile loop, gating, and status handling
// are in place; the transfer worker pool and TGD execution land in a follow-up.
var errTransferNotImplemented = errors.New("transfer execution not yet implemented")

type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver provides repository resolution and caching for the transfer source.
	Resolver *resolution.Resolver

	// PluginManager manages plugins required for transfer operations.
	PluginManager *manager.PluginManager
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Replication{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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

	if replication.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	if !replication.GetDeletionTimestamp().IsZero() {
		// TODO(replication): per ADR 0020 deletion semantics, cancel any in-flight transfer
		// via a per-item context keyed by CR UID and wait for a bounded drain before removing
		// the finalizer. No transfer worker pool exists yet, so we just release the finalizer.
		if updated := controllerutil.RemoveFinalizer(replication, v1alpha1.ReplicationFinalizer); updated {
			if err := r.Update(ctx, replication); err != nil {
				status.MarkNotReady(r.EventRecorder, replication, v1alpha1.DeletionFailedReason, err.Error())

				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}

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
// decides whether a transfer is needed, and hands off to the (not-yet-implemented) Phase 2.
func (r *Reconciler) reconcile(ctx context.Context, replication *v1alpha1.Replication) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: replication.GetNamespace(),
		Name:      replication.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, err.Error())

		var notReadyErr util.NotReadyError
		var deletionErr util.DeletionError
		if errors.As(err, &notReadyErr) || errors.As(err, &deletionErr) {
			logger.Info("source component is not available, waiting for component event", "error", err)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	if component.Status.Component.Digest == nil || component.Status.Component.Digest.Value == "" {
		const msg = "source component digest not yet available"
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, msg)
		logger.Info(msg, "component", replication.Spec.ComponentRef.Name)

		return ctrl.Result{}, nil
	}

	// The target repository must exist and be ready before a transfer can run.
	if _, err := util.GetReadyObject[v1alpha1.Repository, *v1alpha1.Repository](ctx, r.Client, client.ObjectKey{
		Namespace: replication.GetNamespace(),
		Name:      replication.Spec.TargetRepositoryRef.Name,
	}); err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetRepositoryFailedReason, err.Error())

		var notReadyErr util.NotReadyError
		var deletionErr util.DeletionError
		if errors.As(err, &notReadyErr) || errors.As(err, &deletionErr) {
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

	// Persist the effective config immediately so the deferred patch keeps it even if a later step fails.
	if !equality.Semantic.DeepEqual(replication.Status.EffectiveOCMConfig, configs) {
		replication.Status.EffectiveOCMConfig = configs

		return ctrl.Result{}, fmt.Errorf("effective ocm config changed")
	}

	sourceDigest := component.Status.Component.Digest.Value
	replication.Status.Component = component.Status.Component.DeepCopy()

	if sourceDigest == replication.Status.LastTransferredDigest {
		status.RemoveCondition(replication, v1alpha1.TransferInProgressCondition)
		status.SetCondition(replication, metav1.Condition{
			Type:    v1alpha1.TransferInProgressCondition,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.IdleReason,
			Message: "source digest already transferred",
		})
		status.MarkReady(r.EventRecorder, replication, "Successfully transferred component version %s", component.Status.Component.Version)

		return ctrl.Result{}, nil
	}

	status.SetCondition(replication, metav1.Condition{
		Type:    v1alpha1.TransferInProgressCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TransferInProgressReason,
		Message: fmt.Sprintf("transferring component version %s", component.Status.Component.Version),
	})

	// Next: submit the transfer to a dedicated transfer worker pool.
	//
	// TODO(replication): build the in-memory TGD with transfer.BuildGraphDefinition
	// (source resolver from r.Resolver, decoded target repository spec, options derived
	// from spec.TransferConfig)
	// Next: submit it to a dedicated transfer worker pool that returns ErrTransferInProgress
	// for in-flight keys and creates a completion event.
	// Then: The event retriggers this reconcile. On success, set
	// LastTransferredVersion/LastTransferredDigest, clear TransferInProgress, and MarkReady;
	// on failure, clear TransferInProgress and MarkNotReady.
	// Pretty much the same as the component resolution service.
	logger.Info("transfer execution not yet implemented; gating complete",
		"component", component.Status.Component.Component,
		"version", component.Status.Component.Version,
		"sourceDigest", sourceDigest)
	status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, errTransferNotImplemented.Error())

	return ctrl.Result{}, errTransferNotImplemented
}
