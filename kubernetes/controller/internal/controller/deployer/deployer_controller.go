package deployer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
func (r *Reconciler) Untrack(ctx context.Context, deployer *deliveryv1alpha1.Deployer) error {
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
				return fmt.Errorf("context canceled while unregistering resource watch for deployer %s: %w", deployer.Name, ctx.Err())
			}
			atLeastOneResourceNeededStopWatch = true
		}
	}
	if atLeastOneResourceNeededStopWatch {
		return fmt.Errorf("waiting for at least one resource watch to be removed")
	}

	return nil
}

func (r *Reconciler) pruneWithApplySet(ctx context.Context, deployer *deliveryv1alpha1.Deployer) error {
	logger := log.FromContext(ctx).WithValues("deployer", deployer.Name, "namespace", deployer.Namespace)

	set := r.createApplySet(deployer, logger)

	metadata, err := set.Project(nil)
	if err != nil {
		return fmt.Errorf("failed to project ApplySet: %w", err)
	}

	logger.Info("pruning ApplySet", "scope", metadata.PruneScope())
	result, err := set.Prune(ctx, applyset.PruneOptions{
		KeepUIDs:    nil,
		Scope:       metadata.PruneScope(),
		Concurrency: runtime.NumCPU(),
	})
	if err != nil {
		return fmt.Errorf("failed to prune ApplySet: %w", err)
	}

	// Log results
	logger.Info("ApplySet prune operation complete", "pruned", len(result.Pruned))

	// Prune calls delete on every resource found, even if its already being deleted.
	// If we were to remove this check, the deployer might be deleted while a child is stuck in terminating state.
	if !result.HasPruned() {
		logger.Info("pruned resources, doing one more pruning until nothing more to prune")
		return fmt.Errorf("waiting for all resources to be pruned")
	}

	// nothing more to prune, remove finalizer
	return nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	deployer := &deliveryv1alpha1.Deployer{}
	if err := r.Get(ctx, req.NamespacedName, deployer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper := patch.NewSerialPatcher(deployer, r.Client)
	defer func(ctx context.Context) {
		err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, deployer, r.EventRecorder, 0, err))
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

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info("stop reconciling as the resource is not available", "error", err.Error())

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready resource: %w", err)
	}
	if resource.Status.Resource == nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, "resource is empty in status")

		return ctrl.Result{}, fmt.Errorf("failed to get ready resource: %w", err)
	}

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
	return ctrl.Result{RequeueAfter: resource.GetRequeueAfter()}, nil
}

func (r *Reconciler) reconcileDeletionTimestamp(ctx context.Context, deployer *deliveryv1alpha1.Deployer, logger logr.Logger) (ctrl.Result, error, bool) {
	if !deployer.GetDeletionTimestamp().IsZero() {
		var errs []error

		hasPruneSetFinalizer := controllerutil.ContainsFinalizer(deployer, applySetPruneFinalizer)

		if hasPruneSetFinalizer {
			logger.Info("pruning ApplySet before removing finalizer")
			if err := r.pruneWithApplySet(ctx, deployer); err != nil {
				logger.Error(err, "waiting for ApplySet to be pruned before removing finalizer")
				errs = append(errs, err)
			} else {
				logger.Info("successfully pruned ApplySet for deployer")
				controllerutil.RemoveFinalizer(deployer, applySetPruneFinalizer)
			}
		} else if controllerutil.ContainsFinalizer(deployer, resourceWatchFinalizer) {
			logger.Info("untracking resources before removing finalizer")
			if err := r.Untrack(ctx, deployer); err != nil {
				logger.Error(err, "waiting for tracked resources to be unregistered before pruning")
				errs = append(errs, err)
			} else {
				logger.Info("successfully unregistered all resource watches for deployer")
				controllerutil.RemoveFinalizer(deployer, resourceWatchFinalizer)
			}
		}

		if len(errs) > 0 {
			return ctrl.Result{}, fmt.Errorf("failed to cleanup deployer before deletion: %w", errors.Join(errs...)), true
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

	componentDescriptor, err := r.getEffectiveComponentDescriptor(ctx, cacheBackedRepo, deployer, resource, configs)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get effective component descriptor: %w", err)
	}

	resourceIdentity := makeResourceIdentity(resource.Status.Resource)
	var matchedResource *descriptor.Resource
	for i, res := range componentDescriptor.Component.Resources {
		resIdentity := res.ToIdentity()
		if resourceIdentity.Match(resIdentity, identityFunc()) {
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

	blob, err := r.downloadResourceBlob(ctx, cacheBackedRepo, componentDescriptor, matchedResource, cfg)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to download resource: %w", err)
	}
	defer func() {
		err = errors.Join(err, blob.Close())
	}()

	// Decode YAML manifests
	if objs, err = decodeObjectsFromManifest(blob); err != nil {
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
) (io.ReadCloser, error) {
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

		reader, err := blob.ReadCloser()
		if err != nil {
			return nil, fmt.Errorf("failed to get reader from local blob: %w", err)
		}

		return reader, nil
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

	blob, err := resourcePlugin.DownloadResource(ctx, resource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to download resource: %w", err)
	}

	reader, err := blob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get reader from blob: %w", err)
	}

	return reader, nil
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
func identityFunc() ocmruntime.IdentityMatchingChainFn {
	return func(i, o ocmruntime.Identity) bool {
		version, ok := i["version"]
		if !ok || version == "" {
			delete(o, "version")
		}
		return ocmruntime.IdentityEqual(i, o)
	}
}

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
//   - If the resource is from an unverified component version, we can return the component descriptor for the
//     component version referenced in the resource status.
//   - If the resource is from an unverified component version that was resolved via reference path(s), we can return
//     the component descriptor for the component version referenced in the resource status.
//   - If the resource is from a verified component version, we need to make sure that we get the verified component
//     descriptor by adding the verifications from the component CR to the cache-backed repository.
//   - If the resource is from a verified component version that was resolved via reference path(s), we need to get the
//     verified parent component descriptor because the digest specification from the component reference leading to the
//     resolved component version is used create the cache key to maintain the integrity chain.
func (r *Reconciler) getEffectiveComponentDescriptor(
	ctx context.Context,
	repoResource *resolution.CacheBackedRepository,
	deployer *deliveryv1alpha1.Deployer,
	resource *deliveryv1alpha1.Resource,
	configs []deliveryv1alpha1.OCMConfiguration,
) (*descriptor.Descriptor, error) {
	// If the component version name in the resource spec and status are the same, we know that the component version
	// was not resolved through a reference path. We can add verifications from the component CR to the cache-backed
	// repository of the resource and get the component descriptor directly.
	component, err := util.GetReadyObject[deliveryv1alpha1.Component, *deliveryv1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: resource.GetNamespace(),
		Name:      resource.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResourceIsNotAvailable, err.Error())

		return nil, fmt.Errorf("failed to get ready component: %w", err)
	}

	if component.Status.Component.Component == resource.Status.Component.Component {
		verifications, err := ocm.GetVerifications(ctx, r.Client, component)
		if err != nil {
			status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

			return nil, fmt.Errorf("failed to get verifications: %w", err)
		}
		repoResource.Verifications = verifications
		repoResource.SigningRegistry = r.PluginManager.SigningRegistry

		componentDescriptor, err := repoResource.GetComponentVersion(ctx,
			resource.Status.Component.Component,
			resource.Status.Component.Version)
		switch {
		case errors.Is(err, workerpool.ErrResolutionInProgress):
			// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
			status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResolutionInProgress, err.Error())

			return nil, workerpool.ErrResolutionInProgress
		case errors.Is(err, workerpool.ErrNotSafelyDigestible):
			// Ignore error, but log event
			event.New(r.EventRecorder, deployer, nil, eventv1.EventSeverityInfo, err.Error())
		case err != nil:
			status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

			return nil, fmt.Errorf("failed to get component version: %w", err)
		}

		return componentDescriptor, nil
	}

	// If the component name from the component and resource status differ, we know that the component version from the
	// resource status is resolved through a reference path.
	if len(resource.Spec.Resource.ByReference.ReferencePath) == 0 {
		return nil, fmt.Errorf("expected a reference path, but got none")
	}

	// If the component version was resolved through a reference path, we need to make sure getting a component version
	// which integrity chain is intact if the parent component version was verified.
	// To do so, we need to get the parent component version containing the component references. We will use the
	// components repository spec making sure to get the same component version.
	repoComponentSpec := &ocmruntime.Raw{}
	if err := ocmruntime.NewScheme(ocmruntime.WithAllowUnknown()).Decode(
		bytes.NewReader(component.Status.Component.RepositorySpec.Raw), repoComponentSpec); err != nil {
		return nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	componentRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:    repoComponentSpec,
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
		return nil, fmt.Errorf("failed to create cache-backed repository for component: %w", err)
	}

	// Add verifications from the component to the cache-backed repository to make sure they are included in the
	// cache key and used for verification (if any).
	verifications, err := ocm.GetVerifications(ctx, r.Client, component)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get verifications: %w", err)
	}
	componentRepo.Verifications = verifications

	componentDescriptor, err := componentRepo.GetComponentVersion(ctx,
		component.Status.Component.Component,
		component.Status.Component.Version)
	switch {
	case errors.Is(err, workerpool.ErrResolutionInProgress):
		// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResolutionInProgress, err.Error())

		return nil, workerpool.ErrResolutionInProgress
	case errors.Is(err, workerpool.ErrNotSafelyDigestible):
		// Ignore error, but log event
		event.New(r.EventRecorder, deployer, nil, eventv1.EventSeverityInfo, err.Error())
	case err != nil:
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get component version: %w", err)
	}

	// We need to find the matching reference in the parent component descriptor to get the digest specification used
	// for the cache key. We look for the first reference from the resources reference path as this needs to match one
	// reference in the parent component descriptor for the integrity chain to be intact.
	// (The digest spec from the component reference is used to create the cache key for the resolved component version)
	var matchedRef *descriptor.Reference
	for _, parentRef := range componentDescriptor.Component.References {
		parentRefIdentity := parentRef.ToIdentity()

		// The first reference in the reference path needs to match a reference from the parent component.
		if resource.Spec.Resource.ByReference.ReferencePath[0].Match(parentRefIdentity, identityFunc()) {
			matchedRef = &parentRef
			break
		}
	}

	if matchedRef == nil {
		return nil, fmt.Errorf("cannot find matching reference in parent component descriptor for reference path %v",
			resource.Spec.Resource.ByReference.ReferencePath)
	}

	// If the reference has a digest, we need to add it to the repoResource to make sure the cache key is correct and
	// the integrity chain is intact.
	if matchedRef.Digest.Value != "" {
		repoResource.Digest = &v2.Digest{
			HashAlgorithm:          matchedRef.Digest.HashAlgorithm,
			Value:                  matchedRef.Digest.Value,
			NormalisationAlgorithm: matchedRef.Digest.NormalisationAlgorithm,
		}
		repoResource.SigningRegistry = r.PluginManager.SigningRegistry
	}

	componentDescriptor, err = repoResource.GetComponentVersion(ctx,
		resource.Status.Component.Component,
		resource.Status.Component.Version)
	switch {
	case errors.Is(err, workerpool.ErrResolutionInProgress):
		// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ResolutionInProgress, err.Error())

		return nil, workerpool.ErrResolutionInProgress
	case errors.Is(err, workerpool.ErrNotSafelyDigestible):
		// Ignore error, but log event
		event.New(r.EventRecorder, deployer, nil, eventv1.EventSeverityInfo, err.Error())
	case err != nil:
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get component version: %w", err)
	}

	return componentDescriptor, nil
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
