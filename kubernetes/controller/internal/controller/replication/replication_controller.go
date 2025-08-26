package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/ocmutils/check"
	"ocm.software/ocm/api/ocm/tools/transfer"
	"ocm.software/ocm/api/ocm/tools/transfer/transferhandler"
	"ocm.software/ocm/api/ocm/tools/transfer/transferhandler/standard"
	ocmutils "ocm.software/ocm/api/utils/misc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

// Reconciler reconciles a Replication object.
type Reconciler struct {
	*ocm.BaseReconciler

	OCMContextCache *ocm.ContextCache
}

const (
	componentIndexField  = "spec.componentRef.name"
	targetRepoIndexField = "spec.targetRepositoryRef.name"
)

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create index for component name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Replication{}, componentIndexField, componentNameExtractor); err != nil {
		return err
	}

	// Create index for target repository name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Replication{}, targetRepoIndexField, targetRepoNameExtractor); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Replication{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&v1alpha1.Component{}, handler.EnqueueRequestsFromMapFunc(r.replicationMapFunc)).
		Watches(&v1alpha1.Repository{}, handler.EnqueueRequestsFromMapFunc(r.replicationMapFunc)).
		Complete(r)
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

	patchHelper := patch.NewSerialPatcher(replication, r.Client)
	defer func(ctx context.Context) {
		err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, replication, r.EventRecorder, replication.GetRequeueAfter(), err))
	}(ctx)

	if !replication.GetDeletionTimestamp().IsZero() {
		logger.Info("replication is being deleted and cannot be used", "name", replication.Name)

		return ctrl.Result{Requeue: true}, nil
	}

	if replication.Spec.Suspend {
		logger.Info("replication is suspended, skipping reconciliation")

		return ctrl.Result{}, nil
	}

	logger.Info("prepare reconciling replication")

	compNamespace := replication.Spec.ComponentRef.Namespace
	if compNamespace == "" {
		compNamespace = replication.GetNamespace()
	}

	comp, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.GetClient(), client.ObjectKey{
		Namespace: compNamespace,
		Name:      replication.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetResourceFailedReason, "Component is not ready")

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info(err.Error())

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	repoNamespace := replication.Spec.TargetRepositoryRef.Namespace
	if repoNamespace == "" {
		repoNamespace = replication.GetNamespace()
	}

	repo, err := util.GetReadyObject[v1alpha1.Repository, *v1alpha1.Repository](ctx, r.GetClient(), client.ObjectKey{
		Namespace: repoNamespace,
		Name:      replication.Spec.TargetRepositoryRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.GetResourceFailedReason, "OCM repository is not ready")

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info(err.Error())

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready OCM repository: %w", err)
	}

	if conditions.IsReady(replication) &&
		replication.IsInHistory(comp.Status.Component.Component, comp.Status.Component.Version, string(repo.Spec.RepositorySpec.Raw)) {
		status.MarkReady(r.EventRecorder, replication, "Replicated in previous reconciliations: %s to %s", comp.Name, repo.Name)

		return ctrl.Result{RequeueAfter: replication.GetRequeueAfter()}, nil
	}

	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), replication)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), replication, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, err
	}

	historyRecord, err := r.transfer(configs, replication, comp, repo)
	if err != nil {
		historyRecord.Error = err.Error()
		historyRecord.EndTime = metav1.Now()
		r.setReplicationStatus(configs, replication, historyRecord)

		logger.Info("error transferring component", "component", comp.Name, "targetRepository", repo.Name)
		status.MarkNotReady(r.EventRecorder, replication, v1alpha1.ReplicationFailedReason, err.Error())

		return ctrl.Result{}, err
	}

	// Update status
	r.setReplicationStatus(configs, replication, historyRecord)
	status.MarkReady(r.EventRecorder, replication, "Successfully replicated %s to %s", comp.Name, repo.Name)

	return ctrl.Result{RequeueAfter: replication.GetRequeueAfter()}, nil
}

func (r *Reconciler) transfer(
	configs []v1alpha1.OCMConfiguration,
	replication *v1alpha1.Replication,
	comp *v1alpha1.Component,
	targetOCMRepo *v1alpha1.Repository,
) (historyRecord v1alpha1.TransferStatus, retErr error) {
	// DefaultContext is essentially the same as the extended context created here. The difference is, if we
	// register a new type at an extension point (e.g. a new access type), it's only registered at this exact context
	// instance and not at the global default context variable.

	historyRecord = v1alpha1.TransferStatus{
		StartTime:            metav1.Now(),
		Component:            comp.Status.Component.Component,
		Version:              comp.Status.Component.Version,
		SourceRepositorySpec: string(comp.Status.Component.RepositorySpec.Raw),
		TargetRepositorySpec: string(targetOCMRepo.Spec.RepositorySpec.Raw),
	}

	octx, session, err := r.OCMContextCache.GetSession(&ocm.GetSessionOptions{
		RepositorySpecification: comp.Status.Component.RepositorySpec,
		OCMConfigurations:       configs,
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), replication, v1alpha1.ConfigureContextFailedReason, err.Error())

		return historyRecord, fmt.Errorf("failed to get session: %w", err)
	}

	sourceRepo, err := session.LookupRepositoryForConfig(octx, comp.Status.Component.RepositorySpec.Raw)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot lookup repository for RepositorySpec: %w", err)
	}

	cv, err := session.LookupComponentVersion(sourceRepo, comp.Status.Component.Component, comp.Status.Component.Version)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot lookup component version in source repository: %w", err)
	}

	targetCtx, targetSession, err := r.OCMContextCache.GetSession(&ocm.GetSessionOptions{
		RepositorySpecification: targetOCMRepo.Spec.RepositorySpec,
		OCMConfigurations:       configs,
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), replication, v1alpha1.ConfigureContextFailedReason, err.Error())

		return historyRecord, fmt.Errorf("failed to get session: %w", err)
	}

	targetRepo, err := targetSession.LookupRepositoryForConfig(targetCtx, targetOCMRepo.Spec.RepositorySpec.Raw)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot lookup repository for RepositorySpec: %w", err)
	}

	// Extract transfer options from OCM Context
	opts := &standard.Options{}
	err = transferhandler.From(octx, opts)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot retrieve transfer options from OCM context: %w", err)
	}
	err = transferhandler.From(targetCtx, opts)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot retrieve transfer options from OCM context: %w", err)
	}

	err = transfer.Transfer(cv, targetRepo, opts)
	if err != nil {
		return historyRecord, fmt.Errorf("cannot transfer component version to target repository: %w", err)
	}

	// the transfer operation can only be considered successful, if the copied component can be successfully verified in the target repository
	err = r.validate(targetSession, targetRepo, comp.Status.Component.Component, comp.Status.Component.Version)
	if err != nil {
		return historyRecord, err
	}

	historyRecord.Success = true
	historyRecord.EndTime = metav1.Now()

	return historyRecord, nil
}

// validate checks if the component version can be found in the repository
// and if it is completely (with dependent component references) contained in the target OCM repository.
// If this is not the case an error is returned.
// In the future this function should also verify the component's signature.
func (r *Reconciler) validate(session ocmctx.Session, repo ocmctx.Repository, compName string, compVersion string) error {
	// check if component version can be found in the repository
	_, err := session.LookupComponentVersion(repo, compName, compVersion)
	if err != nil {
		return fmt.Errorf("cannot lookup component version in repository: %w", err)
	}

	// 'check.Check()' provides the same functionality as the 'ocm check cv' CLI command.
	// See also: https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_check_componentversions.md
	// TODO: configure '--local-resources' and '--local-sources', if respective transfer options are set
	// (see https://github.com/open-component-model/ocm-project/issues/343)
	result, err := check.Check().ForId(repo, ocmutils.NewNameVersion(compName, compVersion))
	if err != nil {
		return fmt.Errorf("cannot verify that component version exists in repository: %w", err)
	}
	if !result.IsEmpty() {
		msgBytes, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("cannot marshal the result of component version check in repository: %w", err)
		}

		return fmt.Errorf("component version is not completely contained in repository: %s", string(msgBytes))
	}

	// TODO: verify component's signature in target repository (if component is signed)
	// (see https://github.com/open-component-model/ocm-project/issues/344)

	return nil
}

func (r *Reconciler) setReplicationStatus(configs []v1alpha1.OCMConfiguration, replication *v1alpha1.Replication, historyRecord v1alpha1.TransferStatus) {
	replication.AddHistoryRecord(historyRecord)

	replication.Status.EffectiveOCMConfig = configs
}

func componentNameExtractor(obj client.Object) []string {
	replication, ok := obj.(*v1alpha1.Replication)
	if !ok {
		return nil
	}

	return []string{replication.Spec.ComponentRef.Name}
}

func targetRepoNameExtractor(obj client.Object) []string {
	replication, ok := obj.(*v1alpha1.Replication)
	if !ok {
		return nil
	}

	return []string{replication.Spec.TargetRepositoryRef.Name}
}

func (r *Reconciler) replicationMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	var fields client.MatchingFields
	replList := &v1alpha1.ReplicationList{}

	component, ok := obj.(*v1alpha1.Component)
	if ok {
		fields = client.MatchingFields{componentIndexField: component.GetName()}
	} else if repo, ok := obj.(*v1alpha1.Repository); ok {
		fields = client.MatchingFields{targetRepoIndexField: repo.GetName()}
	} else {
		return []reconcile.Request{}
	}

	if err := r.List(ctx, replList, fields); err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(replList.Items))
	for _, replication := range replList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: replication.GetNamespace(),
				Name:      replication.GetName(),
			},
		})
	}

	return requests
}
