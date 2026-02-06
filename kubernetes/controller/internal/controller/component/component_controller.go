package component

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Masterminds/semver/v3"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

// Reconciler reconciles a Component object.
type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver provides repository resolution and caching for component reconciliation.
	// It ensures that repository access is efficient and consistent during reconciliation operations.
	Resolver *resolution.Resolver

	// PluginManager manages signature verification plugins for component version validation.
	// It enables dynamic loading and execution of signature algorithms required for verifying component authenticity.
	PluginManager *manager.PluginManager
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

	// event source from resolver's worker pool to get notified when resolutions complete
	eventSource := workerpool.NewEventSource(r.Resolver.WorkerPool())
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Component{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(eventSource).
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
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), component)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get effective config: %w", err)
	}

	repoSpec := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Decode(bytes.NewReader(repo.Spec.RepositorySpec.Raw), repoSpec); err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	cacheBackedRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:    repoSpec,
		OCMConfigurations: configs,
		Namespace:         component.GetNamespace(),
		RequesterFunc: func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{
				NamespacedName: types.NamespacedName{
					Namespace: component.GetNamespace(),
					Name:      component.GetName(),
				},
			}
		},
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), component, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	version, err := r.DetermineEffectiveVersionFromRepo(ctx, component, cacheBackedRepo)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.CheckVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to determine effective version: %w", err)
	}

	if verifications, err := ocm.GetVerifications(ctx, r.Client, component); err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get verifications: %w", err)
	} else if len(verifications) > 0 {
		cacheBackedRepo.SigningRegistry = r.PluginManager.SigningRegistry
		cacheBackedRepo.Verifications = verifications
	}

	desc, err := cacheBackedRepo.GetComponentVersion(ctx, component.Spec.Component, version)
	switch {
	case errors.Is(err, workerpool.ErrResolutionInProgress):
		// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.ResolutionInProgress, err.Error())
		logger.Info("component version resolution in progress, waiting for event notification",
			"component", component.Spec.Component,
			"version", version)

		return ctrl.Result{}, nil
	case errors.Is(err, workerpool.ErrNotSafelyDigestible):
		// Ignore error, but log event
		event.New(r.EventRecorder, component, nil, eventv1.EventSeverityError, err.Error())
	case err != nil:
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get component version: %w", err)
	}

	digestSpec, err := signing.GenerateDigest(ctx, desc, slog.New(logr.ToSlogHandler(logger)), signing.LegacyNormalisationAlgo, crypto.SHA256.String())
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to generate digest: %w", err)
	}

	logger.Info("updating status")
	component.Status.Component = v1alpha1.ComponentInfo{
		RepositorySpec: repo.Spec.RepositorySpec,
		Component:      component.Spec.Component,
		Version:        version,
		Digest: &v2.Digest{
			HashAlgorithm:          digestSpec.HashAlgorithm,
			NormalisationAlgorithm: digestSpec.NormalisationAlgorithm,
			Value:                  digestSpec.Value,
		},
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

func (r *Reconciler) DetermineEffectiveVersionFromRepo(ctx context.Context, component *v1alpha1.Component,
	repo repository.ComponentVersionRepository,
) (string, error) {
	versions, err := repo.ListComponentVersions(ctx, component.Spec.Component)
	if err != nil {
		return "", fmt.Errorf("failed to list versions: %w", err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("component %s not found in repository", component.Spec.Component)
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
		reconciledcv, err := repo.GetComponentVersion(ctx, component.Spec.Component, currentSemver.Original())
		if err != nil {
			return "", reconcile.TerminalError(fmt.Errorf("failed to get reconciled component version to check"+
				" downgradability: %w", err))
		}

		latestcv, err := repo.GetComponentVersion(ctx, component.Spec.Component, latestSemver.Original())
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
