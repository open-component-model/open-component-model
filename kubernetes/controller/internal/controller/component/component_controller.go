package component

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	mandelerrors "github.com/mandelsoft/goutils/errors"
	"github.com/mandelsoft/goutils/sliceutils"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/compdesc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

// Reconciler reconciles a Component object.
type Reconciler struct {
	*ocm.BaseReconciler

	OCMContextCache *ocm.ContextCache
}

var _ ocm.Reconciler = (*Reconciler)(nil)

var resourceIndex = ".spec.componentRef.Name"

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create index for repository reference name from components to make sure to reconcile, when the base ocm-
	// repository changes.
	const fieldName = "spec.repositoryRef.name"
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Component{}, fieldName, func(obj client.Object) []string {
		component, ok := obj.(*v1alpha1.Component)
		if !ok {
			return nil
		}

		return []string{component.Spec.RepositoryRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	// This index is required to get all resources that reference a component. This is required to make sure that when
	// deleting the component, no resource exists anymore that references that component.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Resource{}, resourceIndex, func(obj client.Object) []string {
		resource, ok := obj.(*v1alpha1.Resource)
		if !ok {
			return nil
		}

		return []string{resource.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Component{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.Repository{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				repository, ok := obj.(*v1alpha1.Repository)
				if !ok {
					return []reconcile.Request{}
				}

				// Get list of components that reference the repository
				list := &v1alpha1.ComponentList{}
				if err := r.List(ctx, list, client.MatchingFields{fieldName: repository.GetName()}); err != nil {
					return []reconcile.Request{}
				}

				// For every component that references the repository create a reconciliation request for that
				// component
				requests := make([]reconcile.Request, 0, len(list.Items))
				for _, component := range list.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: component.GetNamespace(),
							Name:      component.GetName(),
						},
					})
				}

				return requests
			})).
		Watches(
			// Ensure to reconcile the component when an OCM resource changes that references this component.
			// We want to reconcile because the component-finalizer makes sure that the component is only deleted when
			// it is not referenced by any resource anymore. So, when the component is already marked for deletion, we
			// want to get notified about resource changes (e.g. deletion) to remove the component-finalizer
			// respectively.
			&v1alpha1.Resource{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				resource, ok := obj.(*v1alpha1.Resource)
				if !ok {
					return []reconcile.Request{}
				}

				component := &v1alpha1.Component{}
				if err := r.Get(ctx, client.ObjectKey{
					Namespace: resource.GetNamespace(),
					Name:      resource.Spec.ComponentRef.Name,
				}, component); err != nil {
					return []reconcile.Request{}
				}

				// Only reconcile if the component is marked for deletion
				if component.GetDeletionTimestamp().IsZero() {
					return []reconcile.Request{}
				}

				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Namespace: component.GetNamespace(),
						Name:      component.GetName(),
					}},
				}
			})).
		Complete(r)
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=components/finalizers,verbs=update

// +kubebuilder:rbac:groups="",resources=secrets;configmaps;serviceaccounts,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

//nolint:funlen,cyclop // we do not want to cut the function at arbitrary points
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	component := &v1alpha1.Component{}
	if err := r.Get(ctx, req.NamespacedName, component); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper := patch.NewSerialPatcher(component, r.Client)
	defer func(ctx context.Context) {
		err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, component, r.EventRecorder, component.GetRequeueAfter(), err))
	}(ctx)

	if !component.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, component)
	}

	if updated := controllerutil.AddFinalizer(component, v1alpha1.ComponentFinalizer); updated {
		if err := r.Update(ctx, component); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if component.Spec.Suspend {
		logger.Info("component is suspended, skipping reconciliation")

		return ctrl.Result{}, nil
	}

	logger.Info("prepare reconciling component")
	repo, err := util.GetReadyObject[v1alpha1.Repository, *v1alpha1.Repository](ctx, r.Client, client.ObjectKey{
		Namespace: component.GetNamespace(),
		Name:      component.Spec.RepositoryRef.Name,
	})
	if err != nil {
		// Note: Marking the component as not ready, when the repository is not ready is not completely valid. As the
		// component was potentially ready, then the repository changed, but that does not necessarily mean that the
		// component is not ready as well.
		// However, as the component is hard-dependant on the repository, we decided to mark it not ready as well.
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetResourceFailedReason, "OCM Repository is not ready")

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info(err.Error())

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready repository: %w", err)
	}

	logger.Info("reconciling component")
	// DefaultContext is essentially the same as the extended context created here. The difference is, if we
	// register a new type at an extension point (e.g. a new access type), it's only registered at this exact context
	// instance and not at the global default context variable.
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), component)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get effective config: %w", err)
	}

	octx, session, err := r.OCMContextCache.GetSession(&ocm.GetSessionOptions{
		RepositorySpecification: repo.Spec.RepositorySpec,
		OCMConfigurations:       configs,
		VerificationProvider:    component,
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get session: %w", err)
	}
	logger = logger.WithValues("ocmContext", octx.GetId())

	spec, err := octx.RepositorySpecForConfig(repo.Spec.RepositorySpec.Raw, nil)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get repository spec: %w", err)
	}

	// We need a "live", aka not cached, repository access. This is because
	// even though the cached version is mostly good for other controllers, it is the component controllers
	// responsibility to check if the cached version is still valid, or if we need to retrieve a new version.
	// We need to do this because the cached version might be outdated.
	liveRepo, err := octx.RepositoryForSpec(spec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get live repository access: %w", err)
	}
	defer func() {
		err = errors.Join(err, liveRepo.Close())
	}()
	version, err := r.DetermineEffectiveVersionFromLiveRepo(ctx, component, session, liveRepo)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.CheckVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to determine effective version: %w", err)
	}

	repository, err := session.LookupRepository(octx, spec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, "Failed looking up repository")

		return ctrl.Result{}, fmt.Errorf("failed looking up repository: %w", err)
	}
	cv, err := session.LookupComponentVersion(repository, component.Spec.Component, version)
	switch {
	case mandelerrors.IsErrNotFound(err):
		// If we are here, then the component version was not found in the cached session, but in the live repo.
		// In this case we know the session is outdated, so forcefully close it and re-open it on the next reconcile.
		return ctrl.Result{}, fmt.Errorf("cached component version access was not found and cached session needed to be invalidated: %w", errors.Join(err, session.Close()))
	case err != nil:
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get component version: %w", err)
	}

	compareStart := time.Now()
	logger.V(1).Info("comparing cached and live hashes", "component", component.Spec.Component, "version", version)
	digestSpec, err := ocm.CompareCachedAndLiveHashes(cv, liveRepo, component.Spec.Component, version,
		compdesc.JsonNormalisationV3, crypto.SHA256,
	)
	logger.V(1).Info("finished comparing cached and live hashes", "component", component.Spec.Component, "version", version, "duration", time.Since(compareStart))

	switch {
	case errors.Is(err, ocm.ErrUnstableHash):
		event.New(r.EventRecorder, component, nil, eventv1.EventSeverityInfo, err.Error())
		err = nil
	case errors.Is(err, ocm.ErrComponentVersionHashMismatch):
		// If we are here, then the component version was found in the cache, but under a different hash.
		// In this case we know the session is outdated, so forcefully close it and re-open it on the next reconcile.
		log.FromContext(ctx).Info("cached component version access was found, but under a different hash, closing session and re-opening it on the next reconcile")

		return ctrl.Result{}, errors.Join(err, session.Close())
	case err != nil:
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.CheckVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to compare cached and live hashes: %w", err)
	}

	sessionResolver := ocm.NewSessionResolver(octx, session)

	if _, err := ocm.VerifyComponentVersion(ctx, cv, sessionResolver, sliceutils.Transform(component.Spec.Verify, func(verify v1alpha1.Verification) string {
		return verify.Signature
	})); err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to verify component version: %w", err)
	}

	logger.Info("updating status")
	component.Status.Component = v1alpha1.ComponentInfo{
		RepositorySpec: repo.Spec.RepositorySpec,
		Component:      component.Spec.Component,
		Version:        version,
		Digest:         digestSpec,
	}

	component.Status.EffectiveOCMConfig = configs

	status.MarkReady(r.EventRecorder, component, "Applied version %s", version)

	return ctrl.Result{RequeueAfter: component.GetRequeueAfter()}, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, component *v1alpha1.Component) error {
	// The component should only be deleted if no resource exists that references that component.
	resourceList := &v1alpha1.ResourceList{}
	if err := r.List(ctx, resourceList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(
			resourceIndex,
			client.ObjectKeyFromObject(component).Name,
		),
	}); err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, err.Error())

		return fmt.Errorf("failed to list resource: %w", err)
	}

	if len(resourceList.Items) > 0 {
		var names []string
		for _, res := range resourceList.Items {
			names = append(names, fmt.Sprintf("%s/%s", res.Namespace, res.Name))
		}

		msg := fmt.Sprintf(
			"component cannot be removed as resources are still referencing it: %s",
			strings.Join(names, ","),
		)
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, msg)

		return errors.New(msg)
	}

	if updated := controllerutil.RemoveFinalizer(component, v1alpha1.ComponentFinalizer); updated {
		if err := r.Update(ctx, component); err != nil {
			status.MarkNotReady(r.EventRecorder, component, v1alpha1.DeletionFailedReason, err.Error())

			return fmt.Errorf("failed to remove finalizer: %w", err)
		}

		return nil
	}

	status.MarkNotReady(
		r.EventRecorder,
		component,
		v1alpha1.DeletionFailedReason,
		"component is being deleted and still has existing finalizers",
	)

	return nil
}

func (r *Reconciler) DetermineEffectiveVersionFromLiveRepo(ctx context.Context, component *v1alpha1.Component,
	session ocmctx.Session, liveRepo ocmctx.Repository,
) (_ string, err error) {
	liveComponentAccess, err := liveRepo.LookupComponent(component.Spec.Component)
	if err != nil {
		return "", fmt.Errorf("failed to lookup component: %w", err)
	}
	defer func() {
		err = errors.Join(err, liveComponentAccess.Close())
	}()
	versions, err := liveComponentAccess.ListVersions()
	if err != nil {
		return "", fmt.Errorf("failed to list versions: %w", err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("component %s not found in repository", liveComponentAccess.GetName())
	}
	filter, err := ocm.RegexpFilter(component.Spec.SemverFilter)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to parse regexp filter: %w", err))
	}
	latestSemver, err := ocm.GetLatestValidVersion(ctx, versions, component.Spec.Semver, filter)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to get valid latest version: %w", err))
	}

	// we didn't yet reconcile anything, return whatever the retrieved version is.
	if component.Status.Component.Version == "" {
		return latestSemver.Original(), nil
	}

	currentSemver, err := semver.NewVersion(component.Status.Component.Version)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to check reconciled version: %w", err))
	}

	if latestSemver.GreaterThanEqual(currentSemver) {
		return latestSemver.Original(), nil
	}

	switch component.Spec.DowngradePolicy {
	case v1alpha1.DowngradePolicyDeny:
		return "", reconcile.TerminalError(fmt.Errorf("component version cannot be downgraded from version %s "+
			"to version %s", currentSemver.Original(), latestSemver.Original()))
	case v1alpha1.DowngradePolicyEnforce:
		return latestSemver.Original(), nil
	case v1alpha1.DowngradePolicyAllow:
		reconciledcv, err := session.LookupComponentVersion(liveRepo, liveComponentAccess.GetName(), currentSemver.Original())
		if err != nil {
			return "", reconcile.TerminalError(fmt.Errorf("failed to get reconciled component version to check"+
				" downgradability: %w", err))
		}

		latestcv, err := session.LookupComponentVersion(liveRepo, liveComponentAccess.GetName(), latestSemver.Original())
		if err != nil {
			return "", fmt.Errorf("failed to get component version: %w", err)
		}

		downgradable, err := ocm.IsDowngradable(ctx, reconciledcv, latestcv)
		if err != nil {
			return "", reconcile.TerminalError(fmt.Errorf("failed to check downgradability: %w", err))
		}
		if !downgradable {
			// keep requeueing, a greater component version could be published
			// semver constraint may even describe older versions and non-existing newer versions, so you have to check
			// for potential newer versions (current is downgradable to: > 1.0.3, latest is: < 1.1.0, but version 1.0.4
			// does not exist yet, but will be created)
			return "", fmt.Errorf("component version cannot be downgraded from version %s "+
				"to version %s", currentSemver.Original(), latestSemver.Original())
		}

		return latestSemver.Original(), nil
	default:
		return "", reconcile.TerminalError(errors.New("unknown downgrade policy: " + string(component.Spec.DowngradePolicy)))
	}
}
