package deployer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
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

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	deliveryv1alpha1 "ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/applyset"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/dynamic"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/kubernetes/controller/internal/verification"
)

const (
	// resourceWatchFinalizer is the finalizer used to ensure that the resource watch is removed when the deployer is deleted.
	// It is used by the dynamic informer manager to unregister watches for resources that are referenced by the deployer.
	resourceWatchFinalizer = "delivery.ocm.software/watch"

	// applySetPruneFinalizer is the finalizer used to ensure that the ApplySet is pruned when the deployer is deleted.
	applySetPruneFinalizer = "delivery.ocm.software/applyset-prune"

	// deployerManager is the label used to identify the deployer as a manager of resources.
	deployerManager = "deployer.delivery.ocm.software"
)

var ErrComponentVersionDrift = errors.New("component version drift: resource status has not yet caught up with component")

// Reconciler reconciles a Deployer object.
type Reconciler struct {
	*ocm.BaseReconciler

	// resourceWatchChannel is used to register watches for resources that are referenced by the deployer.
	// It is used by the dynamic informer manager to register watches for resources deployed.
	// stopResourceWatchChannel is used to unregister watches for resources that are referenced by the deployer.
	// It is used by the dynamic informer manager to unregister watches when "undeploying" a resource.
	resourceWatchChannel, stopResourceWatchChannel chan dynamic.Event
	// resourceWatchHasSynced is used to check if a resource watch is already registered and synced.
	resourceWatchHasSynced func(parent, obj client.Object) bool
	// resourceWatchIsStopped is used to check if a resource watch is stopped.
	resourceWatchIsStopped func(parent, obj client.Object) bool
	// resourceWatches is used to track the deployed objects and their resource watches.
	// this is used to ensure that the resource watches are removed when the deployer is deleted.
	// Note that technically we also store tracked objects in the status, but to stay idempotent
	// we use a tracker so as to only write to the status, and not read from it.
	resourceWatches func(parent client.Object) []client.Object
	// resourceRESTMapper is the RESTMapper that can be used to introspect resource mappings for dynamic resources
	resourceRESTMapper meta.RESTMapper

	DownloadCache cache.DigestObjectCache[string, []*unstructured.Unstructured]
	Resolver      *resolution.Resolver
	PluginManager *manager.PluginManager

	// MaxResourceSizeBytes is the maximum size in bytes a downloaded resource blob may contain.
	// 0 disables the limit.
	MaxResourceSizeBytes int64
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/finalizers,verbs=update

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	informerManager, err := r.setupDynamicResourceWatcherWithManager(mgr)
	if err != nil {
		return err
	}

	// Build index for deployers that reference a resource to get notified about resource changes.
	const fieldName = ".spec.resourceRef"
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&deliveryv1alpha1.Deployer{},
		fieldName,
		func(obj client.Object) []string {
			deployer, ok := obj.(*deliveryv1alpha1.Deployer)
			if !ok {
				return nil
			}

			return []string{fmt.Sprintf(
				"%s/%s",
				deployer.Spec.ResourceRef.Namespace,
				deployer.Spec.ResourceRef.Name,
			)}
		},
	); err != nil {
		return err
	}

	eventSource := workerpool.NewEventSource(r.Resolver.WorkerPool())
	return ctrl.NewControllerManagedBy(mgr).
		For(&deliveryv1alpha1.Deployer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(eventSource).
		WatchesRawSource(informerManager.Source()).
		// Watch for events from OCM resources that are referenced by the deployer
		Watches(
			&deliveryv1alpha1.Resource{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				resource, ok := obj.(*deliveryv1alpha1.Resource)
				if !ok {
					return []reconcile.Request{}
				}

				// Get list of deployers that reference the resource
				list := &deliveryv1alpha1.DeployerList{}
				if err := r.List(
					ctx,
					list,
					client.MatchingFields{fieldName: client.ObjectKeyFromObject(resource).String()},
				); err != nil {
					return []reconcile.Request{}
				}

				// For every deployer that references the resource create a reconciliation request for that deployer
				requests := make([]reconcile.Request, 0, len(list.Items))
				for _, deployer := range list.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: k8stypes.NamespacedName{
							Namespace: deployer.GetNamespace(),
							Name:      deployer.GetName(),
						},
					})
				}

				return requests
			})).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 5*time.Minute),
		}).
		Complete(r)
}

func (r *Reconciler) setupDynamicResourceWatcherWithManager(mgr ctrl.Manager) (*dynamic.InformerManager, error) {
	// only register watches for resources that are managed by the deployer controller
	sel, err := labels.Parse(fmt.Sprintf("%s=%s", managedByLabel, deployerManager))
	if err != nil {
		return nil, fmt.Errorf("failed to parse label selector: %w", err)
	}

	const channelBufferSize = 10

	// For Registering and Unregistering watches, we use a dynamic informer manager.
	// To buffer pending registrations and unregistrations, we use channels.
	informerManager, err := dynamic.NewInformerManager(&dynamic.Options{
		Config:     mgr.GetConfig(),
		HTTPClient: mgr.GetHTTPClient(),
		RESTMapper: mgr.GetRESTMapper(),
		Handler: handler.EnqueueRequestForOwner(
			mgr.GetScheme(), mgr.GetRESTMapper(),
			&deliveryv1alpha1.Deployer{},
			handler.OnlyControllerOwner(),
		),
		DefaultLabelSelector:        sel,
		Workers:                     runtime.NumCPU(),
		RegisterChannelBufferSize:   channelBufferSize,
		UnregisterChannelBufferSize: channelBufferSize,
		MetricsLabel:                deployerManager + "/" + "resources",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic informer deployerManager: %w", err)
	}

	// this channel is used to register watches for resources that are referenced by the deployer.
	r.resourceWatchChannel = informerManager.RegisterChannel()
	// this channel is used to unregister watches for resources that are referenced by the deployer.
	r.stopResourceWatchChannel = informerManager.UnregisterChannel()
	// The resourceWatchHasSynced function is used to check if a resource is already registered and synced once requested.
	r.resourceWatchHasSynced = informerManager.HasSynced
	// The resourceWatchIsStopped function is used to check if a resource watch is stopped. useful for cleanup purposes.
	r.resourceWatchIsStopped = informerManager.IsStopped
	r.resourceWatches = informerManager.ActiveForParent
	r.resourceRESTMapper = informerManager.RESTMapper()
	// Add the dynamic informer deployerManager to the controller deployerManager. This will make the dynamic informer deployerManager start
	// its registration and unregistration workers once the controller deployerManager is started.
	if err := mgr.Add(informerManager); err != nil {
		return nil, fmt.Errorf("failed to add dynamic informer deployerManager to controller deployerManager: %w", err)
	}

	return informerManager, nil
}

// Untrack removes the deployer from the tracked objects and stops the resource watch if it is still running.
// It also removes the finalizer from the deployer if there are no more tracked objects.
// Untrack sends stop events for all active resource watches on the deployer.
// Returns true if all watches are already stopped, false if stop events were
// sent and another reconcile is needed to verify completion.
func (r *Reconciler) Untrack(ctx context.Context, deployer *deliveryv1alpha1.Deployer) (bool, error) {
	logger := log.FromContext(ctx)
	var atLeastOneResourceNeededStopWatch bool
	for _, obj := range r.resourceWatches(deployer) {
		if !r.resourceWatchIsStopped(deployer, obj) {
			logger.Info("unregistering resource watch for deployer", "name", deployer.GetName())
			select {
			case r.stopResourceWatchChannel <- dynamic.Event{
				Parent: deployer,
				Child:  obj,
			}:
			case <-ctx.Done():
				return false, fmt.Errorf("context canceled while unregistering resource watch for deployer %s: %w", deployer.Name, ctx.Err())
			}
			atLeastOneResourceNeededStopWatch = true
		}
	}
	if atLeastOneResourceNeededStopWatch {
		return false, nil
	}

	return true, nil
}

// pruneWithApplySet prunes all resources managed by the deployer's ApplySet.
// Returns true if pruning is complete (nothing left to prune), false if resources
// are still being pruned and another reconcile is needed.
func (r *Reconciler) pruneWithApplySet(ctx context.Context, deployer *deliveryv1alpha1.Deployer) (bool, error) {
	logger := log.FromContext(ctx).WithValues("deployer", deployer.Name, "namespace", deployer.Namespace)

	set := r.createApplySet(deployer, logger)

	metadata, err := set.Project(nil)
	if err != nil {
		return false, fmt.Errorf("failed to project ApplySet: %w", err)
	}

	logger.Info("pruning ApplySet", "scope", metadata.PruneScope())
	result, err := set.Prune(ctx, applyset.PruneOptions{
		KeepUIDs:    nil,
		Scope:       metadata.PruneScope(),
		Concurrency: runtime.NumCPU(),
	})
	if err != nil {
		return false, fmt.Errorf("failed to prune ApplySet: %w", err)
	}

	logger.Info("ApplySet prune operation complete", "pruned", len(result.Pruned))

	if result.HasPruned() {
		logger.Info("resources still being pruned, waiting for them to be fully removed")
		return false, nil
	}

	return true, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	deployer := &deliveryv1alpha1.Deployer{}
	if err := r.Get(ctx, req.NamespacedName, deployer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	old := deployer.DeepCopy()
	defer func(ctx context.Context) {
		if !equality.Semantic.DeepEqual(deployer.Finalizers, old.Finalizers) {
			err = errors.Join(err, r.GetClient().Update(ctx, deployer))
			return
		}
		status.UpdateBeforePatch(deployer, r.EventRecorder, 0, err)
		if !equality.Semantic.DeepEqual(deployer.Status, old.Status) {
			err = errors.Join(err, r.GetClient().Status().Patch(ctx, deployer, client.MergeFrom(old)))
		}
	}(ctx)

	if deployer.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	result, err, needsDeletion := r.reconcileDeletionTimestamp(ctx, deployer, logger)
	if needsDeletion {
		return result, err
	}

	resourceNamespace := deployer.Spec.ResourceRef.Namespace
	if resourceNamespace == "" {
		resourceNamespace = deployer.GetNamespace()
	}

	resource, err := util.GetReadyObject[deliveryv1alpha1.Resource, *deliveryv1alpha1.Resource](ctx, r.Client, client.ObjectKey{
		Namespace: resourceNamespace,
		Name:      deployer.Spec.ResourceRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, err.Error())

		var notReadyErr util.NotReadyError
		var deletionErr util.DeletionError
		if errors.As(err, &notReadyErr) || errors.As(err, &deletionErr) {
			logger.Info("resource is not available", "error", err)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready resource: %w", err)
	}
	if resource.Status.Resource == nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, "resource is empty in status")

		return ctrl.Result{}, fmt.Errorf("failed to get ready resource: %w", err)
	}

	// TODO: This is suboptimal because we cannot ensure that the resource status will hold a digest (e.g. for unverified
	//       component version/skipped verification). Instead we might use the cache-key from the resolution service.
	key := resource.Status.Resource.Digest.Value

	objs, err := r.DownloadCache.Load(key, func() ([]*unstructured.Unstructured, error) {
		return r.DownloadResourceWithOCM(ctx, deployer, resource)
	})
	if errors.Is(err, resolution.ErrResolutionInProgress) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to download resource from OCM or retrieve it from the cache: %w", err)
	}

	if err = r.applyWithApplySet(ctx, resource, deployer, objs); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ApplyFailed, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to apply resources: %w", err)
	}

	// Track the applied objects for the dynamic informer manager
	if err = r.trackConcurrently(ctx, deployer, objs); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceNotSynced, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to sync deployed resources: %w", err)
	}

	updateDeployedObjectStatusReferences(objs, deployer)
	// TODO: move finalizer up because removal is anyhow idempotent
	controllerutil.AddFinalizer(deployer, applySetPruneFinalizer)
	controllerutil.AddFinalizer(deployer, resourceWatchFinalizer)

	// TODO: Status propagation of RGD status to deployer
	//       (see https://github.com/open-component-model/ocm-k8s-toolkit/issues/192)
	status.MarkReady(r.EventRecorder, deployer, "Applied version %s", resource.Status.Resource.Version)

	// we requeue the deployer after the requeue time specified in the resource.
	return status.RequeueResult(deployer, resource.GetRequeueAfter()), nil
}

func (r *Reconciler) reconcileDeletionTimestamp(ctx context.Context, deployer *deliveryv1alpha1.Deployer, logger logr.Logger) (ctrl.Result, error, bool) {
	if !deployer.GetDeletionTimestamp().IsZero() {
		var errs []error

		hasPruneSetFinalizer := controllerutil.ContainsFinalizer(deployer, applySetPruneFinalizer)

		if hasPruneSetFinalizer {
			logger.Info("pruning ApplySet before removing finalizer")
			done, err := r.pruneWithApplySet(ctx, deployer)
			switch {
			case err != nil:
				logger.Error(err, "waiting for ApplySet to be pruned before removing finalizer")
				errs = append(errs, err)
			case !done:
				return ctrl.Result{RequeueAfter: time.Second}, nil, true
			default:
				logger.Info("successfully pruned ApplySet for deployer")
				controllerutil.RemoveFinalizer(deployer, applySetPruneFinalizer)
			}
		} else if controllerutil.ContainsFinalizer(deployer, resourceWatchFinalizer) {
			logger.Info("untracking resources before removing finalizer")
			done, err := r.Untrack(ctx, deployer)
			switch {
			case err != nil:
				logger.Error(err, "waiting for tracked resources to be unregistered before pruning")
				errs = append(errs, err)
			case !done:
				return ctrl.Result{RequeueAfter: time.Second}, nil, true
			default:
				logger.Info("successfully untracked resources")
				controllerutil.RemoveFinalizer(deployer, resourceWatchFinalizer)
			}
		}

		if len(errs) > 0 {
			return ctrl.Result{}, fmt.Errorf("failed to cleanup deployer before deletion: %w", errors.Join(errs...)), true
		}

		// Requeue if there are still finalizers to process. Metadata-only changes
		// (like finalizer removal) do not trigger the GenerationChangedPredicate,
		// so an explicit requeue is needed.
		if controllerutil.ContainsFinalizer(deployer, applySetPruneFinalizer) ||
			controllerutil.ContainsFinalizer(deployer, resourceWatchFinalizer) {
			return ctrl.Result{Requeue: true}, nil, true
		}

		logger.Info("successfully cleaned up deployer before deletion")
		return ctrl.Result{}, nil, true
	}
	return ctrl.Result{}, nil, false
}

func (r *Reconciler) DownloadResourceWithOCM(
	ctx context.Context,
	deployer *deliveryv1alpha1.Deployer,
	resource *deliveryv1alpha1.Resource,
) (objs []*unstructured.Unstructured, err error) {
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), deployer, resource)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), deployer, deliveryv1alpha1.ConfigureContextFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get effective config: %w", err)
	}
	deployer.Status.EffectiveOCMConfig = configs

	repoSpec := &ocmruntime.Raw{}
	if err := ocmruntime.NewScheme(ocmruntime.WithAllowUnknown()).Decode(
		bytes.NewReader(resource.Status.Component.RepositorySpec.Raw), repoSpec); err != nil {
		status.MarkNotReady(r.GetEventRecorder(), deployer, deliveryv1alpha1.GetRepositoryFailedReason, err.Error())

		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	cacheBackedRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:    repoSpec,
		OCMConfigurations: configs,
		Namespace:         deployer.GetNamespace(),
		SigningRegistry:   r.PluginManager.SigningRegistry,
		RequesterFunc: func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: deployer.GetNamespace(),
					Name:      deployer.GetName(),
				},
			}
		},
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), deployer, deliveryv1alpha1.GetRepositoryFailedReason, err.Error())

		return nil, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	componentDescriptor, err := r.getEffectiveComponentDescriptor(ctx, deployer, resource, configs)
	switch {
	case errors.Is(err, workerpool.ErrResolutionInProgress):
		// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResolutionInProgress, err.Error())

		return nil, workerpool.ErrResolutionInProgress
	case errors.Is(err, ErrComponentVersionDrift):
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ComponentDriftResolutionInProgress, err.Error())

		return nil, ErrComponentVersionDrift
	case errors.Is(err, workerpool.ErrNotSafelyDigestible):
		// Ignore error, but log event
		event.New(r.EventRecorder, deployer, nil, deliveryv1alpha1.EventSeverityError, err.Error())
	default:
		if err != nil {
			status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

			return nil, fmt.Errorf("failed to get effective component version: %w", err)
		}
	}

	resourceIdentity := makeResourceIdentity(resource.Status.Resource)
	var matchedResource *descriptor.Resource
	for i, res := range componentDescriptor.Component.Resources {
		resIdentity := res.ToIdentity()
		if resourceIdentity.Match(resIdentity, ocm.IdentityFuncIgnoreVersion()) {
			matchedResource = &componentDescriptor.Component.Resources[i]
			break
		}
	}

	if matchedResource == nil {
		err := fmt.Errorf("resource with identity %v not found in component %s:%s",
			resourceIdentity, componentDescriptor.Component.Name, componentDescriptor.Component.Version)
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, err
	}

	cfg, err := configuration.LoadConfigurations(ctx, r.Client, deployer.GetNamespace(), configs)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to load configurations: %w", err)
	}

	resourceBlob, err := r.downloadResourceBlob(ctx, cacheBackedRepo, componentDescriptor, matchedResource, cfg)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to download resource: %w", err)
	}

	limitedReader, err := resourceBlob.ReadCloser()
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("getting reader for resource blob: %w", err)
	}
	defer func() {
		err = errors.Join(err, limitedReader.Close())
	}()

	// Enforce resource size limit: opportunistic pre-check using declared size,
	// then wrap the reader to cap reads at the limit.
	// This only happens when a maximum resource limit is set (> 0). Otherwise, we will read the whole resource
	// regardless of its size.
	if r.MaxResourceSizeBytes > 0 {
		if sizeAware, ok := resourceBlob.(blob.SizeAware); ok {
			if size := sizeAware.Size(); size != blob.SizeUnknown && size > r.MaxResourceSizeBytes {
				err := fmt.Errorf("resource size %s exceeds maximum allowed size of %s",
					apiresource.NewQuantity(size, apiresource.BinarySI),
					apiresource.NewQuantity(r.MaxResourceSizeBytes, apiresource.BinarySI),
				)
				status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

				return nil, err
			}
		}

		limitedReader = &limitedReadCloser{Closer: limitedReader, limited: &io.LimitedReader{R: limitedReader, N: r.MaxResourceSizeBytes}}
	}

	// Decode YAML manifests
	if objs, err = decodeObjectsFromManifest(limitedReader); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.MarshalFailedReason, err.Error())

		return nil, fmt.Errorf("failed to decode objects: %w", err)
	}

	return objs, nil
}

func decodeObjectsFromManifest(manifest io.ReadCloser) (_ []*unstructured.Unstructured, err error) {
	const bufferSize = 4096
	decoder := yaml.NewYAMLOrJSONDecoder(manifest, bufferSize)
	var objs []*unstructured.Unstructured
	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
		}
		objs = append(objs, &obj)
	}

	if len(objs) == 0 {
		return nil, fmt.Errorf("no objects found in  manifest")
	}

	return objs, nil
}

// downloadResourceBlob downloads a resource blob using either the repository (for local blobs)
// or the plugin manager (for external access types like OCI images).
func (r *Reconciler) downloadResourceBlob(
	ctx context.Context,
	repo *resolution.CacheBackedRepository,
	componentDescriptor *descriptor.Descriptor,
	resource *descriptor.Resource,
	cfg *configuration.Configuration,
) (blob.ReadOnlyBlob, error) {
	typed, err := v2.Scheme.NewObject(resource.Access.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve access type: %w", err)
	}

	switch typed.(type) { //nolint:gocritic // no, I like switch for types better
	case *v2.LocalBlob:
		blob, _, err := repo.GetLocalResource(ctx,
			componentDescriptor.Component.Name,
			componentDescriptor.Component.Version,
			resource.ToIdentity())
		if err != nil {
			return nil, fmt.Errorf("failed to get local resource: %w", err)
		}

		return blob, nil
	}

	// non-local access types use the plugin manager
	resourcePlugin, err := r.PluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource plugin: %w", err)
	}

	creds, err := resolveResourceCredentials(ctx, r.PluginManager, resource, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}

	return resourcePlugin.DownloadResource(ctx, resource, creds)
}

// resolveResourceCredentials resolves credentials for accessing a resource.
func resolveResourceCredentials(
	ctx context.Context,
	pm *manager.PluginManager,
	resource *descriptor.Resource,
	cfg *configuration.Configuration,
) (map[string]string, error) {
	if cfg == nil {
		return nil, nil
	}

	resourcePlugin, err := pm.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource plugin: %w", err)
	}

	id, err := resourcePlugin.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource credential consumer identity: %w", err)
	}

	logger := log.FromContext(ctx)
	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: pm,
		Logger:        &logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	creds, err := credGraph.Resolve(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}

	return creds, nil
}

// makeResourceIdentity creates a runtime.Identity from a ResourceInfo.
func makeResourceIdentity(info *deliveryv1alpha1.ResourceInfo) ocmruntime.Identity {
	identity := ocmruntime.Identity{
		"name": info.Name,
	}

	if info.Version != "" {
		identity["version"] = info.Version
	}

	for k, v := range info.ExtraIdentity {
		identity[k] = v
	}

	return identity
}

// identityFunc is a custom identity matching function that ignores the "version" field if it is not set.

func (r *Reconciler) createApplySet(deployer *deliveryv1alpha1.Deployer, logger logr.Logger) *applyset.ApplySet {
	cfg := applyset.Config{
		Client:          r.Client,
		RESTMapper:      r.resourceRESTMapper,
		Log:             logger,
		ParentNamespace: deployer.GetNamespace(),
	}
	return applyset.New(cfg, deployer)
}

// applyWithApplySet applies the resource objects using ApplySet for proper tracking and pruning.
// This method uses the ApplySet specification (KEP-3659) to manage sets of resources with automatic
// pruning of orphaned resources.
//
// The deployer object itself is used as the ApplySet parent, which means:
// - All deployed resources are labeled with applyset.k8s.io/part-of=<applyset-id>
// - The deployer carries annotations tracking the GroupKinds and namespaces of managed resources
// - Pruning automatically removes resources that were previously deployed but are no longer in the manifest
func (r *Reconciler) applyWithApplySet(ctx context.Context, resource *deliveryv1alpha1.Resource, deployer *deliveryv1alpha1.Deployer, objs []*unstructured.Unstructured) error {
	logger := log.FromContext(ctx).WithValues("deployer", deployer.Name, "namespace", deployer.Namespace)

	// Use the deployer as the ApplySet parent
	// This allows us to track all resources deployed by this deployer
	set := r.createApplySet(deployer, logger)

	logger.Info("adding objects to ApplySet", "count", len(objs))

	resourcesToAdd := make([]applyset.Resource, 0, len(objs))
	// Add all objects to the ApplySet
	for _, obj := range objs {
		// Clone the object to avoid modifying the original
		obj := obj.DeepCopy()

		// Set ownership labels and annotations (preserving existing behavior)
		setOwnershipLabels(obj, resource, deployer)
		logger.Info("set ownership labels", "labels", obj.GetLabels())
		setOwnershipAnnotations(obj, resource)
		logger.Info("set ownership annotations", "annotations", obj.GetAnnotations())

		// Set controller reference
		if err := controllerutil.SetControllerReference(deployer, obj, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference on object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}

		// Default namespace and apiVersion if needed
		if err := r.defaultObj(ctx, resource, obj); err != nil {
			return fmt.Errorf("failed to default object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}

		resourcesToAdd = append(resourcesToAdd, applyset.Resource{
			ID:        obj.GetName(),
			Object:    obj,
			SkipApply: false,
		})
	}

	logger.Info("projecting ApplySet and set deployer metadata")
	metadata, err := set.Project(resourcesToAdd)
	if err != nil {
		return fmt.Errorf("failed to project ApplySet: %w", err)
	}

	if err := r.setApplySetMetadata(ctx, deployer, metadata); err != nil {
		return fmt.Errorf("failed to set ApplySet metadata on deployer: %w", err)
	}

	logger.Info("applying ApplySet")
	applyResult, metadata, err := set.Apply(ctx, resourcesToAdd, applyset.ApplyMode{Concurrency: runtime.NumCPU()})
	if err != nil {
		return fmt.Errorf("failed to apply ApplySet: %w", err)
	}

	if applyResult.Errors() != nil {
		return fmt.Errorf("errors occurred during ApplySet apply: %w", applyResult.Errors())
	}

	// Log results
	logger.Info("ApplySet operation complete", "applied", len(applyResult.Applied))

	pruneResult, err := set.Prune(ctx, applyset.PruneOptions{
		KeepUIDs:    applyResult.ObservedUIDs(),
		Scope:       metadata.PruneScope(),
		Concurrency: runtime.NumCPU(),
	})
	if err != nil {
		return fmt.Errorf("failed to prune ApplySet: %w", err)
	}

	// Log prune results
	logger.Info("ApplySet prune operation complete", "pruned", len(pruneResult.Pruned))

	return nil
}

// defaultObj ensures an unstructured object has consistent API metadata before being applied.
// It performs defaulting for namespace and apiVersion based on the cluster REST mapping.
//
// Behavior:
//  1. Determines the GroupVersionKind (GVK) using the RESTMapper that is dynamically filled.
//  2. If the object is namespaced but lacks a namespace, it defaults to "default" and logs the action.
//  3. If the object's apiVersion is missing but the RESTMapper provides one, it applies that version.
func (r *Reconciler) defaultObj(ctx context.Context, resource *deliveryv1alpha1.Resource, obj *unstructured.Unstructured) error {
	logger := log.FromContext(ctx).WithValues(
		"operation", "apply",
		"gvk", obj.GetObjectKind().GroupVersionKind().String())

	// now we default the namespace in case we do not have it from the base object.
	gvk := schema.FromAPIVersionAndKind(obj.GetAPIVersion(), obj.GetKind())
	mapping, err := r.resourceRESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("failed to determine resource mapping: %w", err)
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace && obj.GetNamespace() == "" {
		// TODO(jakobmoellerdev) we can think of adding more namespacing options down the line
		logger.Info("namespace will be defaulted", "defaultNamespace", resource.GetNamespace())
		obj.SetNamespace(metav1.NamespaceDefault)
	}
	if gvk.Version == "" && mapping.GroupVersionKind.Version != "" {
		logger.Info("apiVersion will be defaulted to match discovered rest mapping", "defaultAPIVersion", mapping.GroupVersionKind.Version)
		gvk.Version = mapping.GroupVersionKind.Version
		obj.SetGroupVersionKind(gvk)
	}
	return nil
}

// trackConcurrently tracks the objects for the deployer concurrently.
//
// See track for more details on how the objects are tracked.
func (r *Reconciler) trackConcurrently(ctx context.Context, deployer *deliveryv1alpha1.Deployer, objs []*unstructured.Unstructured) error {
	eg, egctx := errgroup.WithContext(ctx)

	for i := range objs {
		eg.Go(func() error {
			return r.track(egctx, deployer, objs[i])
		})
	}

	return eg.Wait()
}

// track registers the object for the deployer and tracks it.
// It checks if the resource watch is already registered and synced. If not, it registers the watch and returns an error
// indicating that the object is not yet registered and synced.
// If the resource watch is already registered and synced, it skips the registration and returns nil.
func (r *Reconciler) track(ctx context.Context, deployer *deliveryv1alpha1.Deployer, obj client.Object) error {
	logger := log.FromContext(ctx)

	if r.resourceWatchHasSynced(deployer, obj) {
		logger.Info("object is already registered and synced, skipping registration")
	} else {
		logger.Info("registering watch from deployer", "obj", obj.GetName())
		select {
		case r.resourceWatchChannel <- dynamic.Event{
			Parent: deployer,
			Child:  obj,
		}:
		case <-ctx.Done():
			return fmt.Errorf("context canceled while unregistering resource watch for deployer %s: %w", deployer.Name, ctx.Err())
		}

		return fmt.Errorf("object is not yet registered and synced, waiting for registration")
	}

	return nil
}

// getEffectiveComponentDescriptor retrieves the effective component descriptor for the resource.
// The resource status tells us which component version was resolved for the resource. However, making sure the
// integrity of that component version is still intact is tricky.
//
//   - If the resource is from the same component version as the component from the component CR, we need to check for
//     verifications on the component CR and add them to the cache-backed repository to make sure they are included in
//     the cache key and used for verification (if any).
//   - If the resource is from a component version that was resolved through a reference path in the resource
//     controller, we need to resolve the path again starting from the component specified in the component CR, to make
//     sure we get the same component version with an intact integrity chain (if a digest was provided to check it).
//     This operation should be cheap as we expect the component to be in cache already.
func (r *Reconciler) getEffectiveComponentDescriptor(
	ctx context.Context,
	deployer *deliveryv1alpha1.Deployer,
	resource *deliveryv1alpha1.Resource,
	configs []deliveryv1alpha1.OCMConfiguration,
) (*descriptor.Descriptor, error) {
	// We get the (ready) component CR to (1) get any verifications needed to resolve the component version and (2) to
	// compare the component version used in the component and resource controller.
	component, err := util.GetReadyObject[deliveryv1alpha1.Component, *deliveryv1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: resource.GetNamespace(),
		Name:      resource.Spec.ComponentRef.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get ready component: %w", err)
	}

	repoSpecComponent := &ocmruntime.Raw{}
	if err := ocmruntime.NewScheme(ocmruntime.WithAllowUnknown()).Decode(
		bytes.NewReader(component.Status.Component.RepositorySpec.Raw), repoSpecComponent); err != nil {
		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	// Add verifications from the component to the cache-backed repository to make sure they are included in the
	// cache key and used for verification (if any).
	verifications, err := verification.GetVerifications(ctx, r.Client, component)
	if err != nil {
		return nil, fmt.Errorf("failed to get verifications: %w", err)
	}

	requesterFunc := func() workerpool.RequesterInfo {
		return workerpool.RequesterInfo{
			NamespacedName: k8stypes.NamespacedName{
				Namespace: deployer.GetNamespace(),
				Name:      deployer.GetName(),
			},
		}
	}

	verifiedOpts := resolution.RepositoryOptions{
		RepositorySpec:    repoSpecComponent,
		OCMConfigurations: configs,
		Namespace:         component.GetNamespace(),
		SigningRegistry:   r.PluginManager.SigningRegistry,
		Verifications:     verifications,
		RequesterFunc:     requesterFunc,
	}

	refPathOpts := resolution.RepositoryOptions{
		RepositorySpec:    repoSpecComponent,
		OCMConfigurations: configs,
		Namespace:         deployer.GetNamespace(),
		SigningRegistry:   r.PluginManager.SigningRegistry,
		RequesterFunc:     requesterFunc,
	}

	repoComponent, err := r.Resolver.NewCacheBackedRepository(ctx, &verifiedOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	componentDescriptorComponent, err := repoComponent.GetComponentVersion(ctx,
		component.Status.Component.Component,
		component.Status.Component.Version)
	// Only return the error if it is not of type `ErrNotSafelyDigestible`. If it is of that type, we need to check
	// if there is a reference path to resolve.
	if err != nil && !errors.Is(err, workerpool.ErrNotSafelyDigestible) {
		return nil, fmt.Errorf("failed to get component version from cache-backed repository: %w", err)
	}

	// Early return if the component version from the component and resource status are the same.
	// Returning the error as well as this could be of type `ErrNotSafelyDigestible`, which is a fallthrough with
	// an error event. The error will be checked in the calling code.
	if component.Status.Component.Component == resource.Status.Component.Component &&
		component.Status.Component.Version == resource.Status.Component.Version {
		return componentDescriptorComponent, err
	}

	// If the component CR got an update while the deployer reconciler is already running, it is possible that the
	// component version in the component and resource CR are different, but the component version from the resource
	// status is not resolved through a reference path.
	// In this case we do nothing and wait for the resource.
	if len(resource.Spec.Resource.ByReference.ReferencePath) == 0 {
		log.FromContext(ctx).Info("component version from resource status differs from component version from component, but no reference path provided",
			"resourceComponent", resource.Status.Component.Component,
			"resourceVersion", resource.Status.Component.Version,
			"componentComponent", component.Status.Component.Component,
			"componentVersion", component.Status.Component.Version)

		return nil, ErrComponentVersionDrift
	}

	resourceDescriptor, _, errReferencePath := ocm.ResolveReferencePath(
		ctx,
		r.Resolver,
		componentDescriptorComponent,
		resource.Spec.Resource.ByReference.ReferencePath,
		&refPathOpts,
	)
	if errReferencePath != nil && !errors.Is(errReferencePath, workerpool.ErrNotSafelyDigestible) {
		return nil, fmt.Errorf("failed to resolve resource reference path: %w", errReferencePath)
	}

	// Join potential errors of type ErrNotSafelyDigestible to be handled by the calling function.
	err = errors.Join(err, errReferencePath)

	return resourceDescriptor, err
}

func updateDeployedObjectStatusReferences[T client.Object](objs []T, deployer *deliveryv1alpha1.Deployer) {
	for _, obj := range objs {
		apiVersion, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
		ref := deliveryv1alpha1.DeployedObjectReference{
			APIVersion: apiVersion,
			Kind:       kind,
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
			UID:        obj.GetUID(),
		}
		if idx := slices.IndexFunc(deployer.Status.Deployed, func(reference deliveryv1alpha1.DeployedObjectReference) bool {
			return reference.UID == obj.GetUID()
		}); idx < 0 {
			deployer.Status.Deployed = append(deployer.Status.Deployed, ref)
		} else {
			deployer.Status.Deployed[idx] = ref
		}
	}
}
