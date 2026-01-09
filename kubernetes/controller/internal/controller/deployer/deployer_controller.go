package deployer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"

	"github.com/fluxcd/pkg/runtime/patch"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ocmctx "ocm.software/ocm/api/ocm"
	ocmv1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/extensions/attrs/signingattr"
	"ocm.software/ocm/api/ocm/selectors/rscsel"
	"ocm.software/ocm/api/ocm/tools/signing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deliveryv1alpha1 "ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/applyset"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/dynamic"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

const (
	// resourceWatchFinalizer is the finalizer used to ensure that the resource watch is removed when the deployer is deleted.
	// It is used by the dynamic informer manager to unregister watches for resources that are referenced by the deployer.
	resourceWatchFinalizer = "delivery.ocm.software/watch"
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

	OCMContextCache *ocm.ContextCache
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kro.run,resources=resourcegraphdefinitions,verbs=get;list;watch;create;update;patch

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

	return ctrl.NewControllerManagedBy(mgr).
		For(&deliveryv1alpha1.Deployer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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
						NamespacedName: types.NamespacedName{
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

	controllerutil.RemoveFinalizer(deployer, resourceWatchFinalizer)

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

	if !deployer.GetDeletionTimestamp().IsZero() {
		if err := r.Untrack(ctx, deployer); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to untrack deployer: %w", err)
		}

		return ctrl.Result{}, fmt.Errorf("deployer is being deleted, waiting for resource watches to be removed")
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

	// Download the resource
	key := resource.Status.Resource.Digest.Value

	objs, err := r.DownloadCache.Load(key, func() ([]*unstructured.Unstructured, error) {
		return r.DownloadResourceWithOCM(ctx, deployer, resource)
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to download resource from OCM or retrieve it from the cache: %w", err)
	}

	// TODO(matthiasbruns)
	// If needed in the future, we can make pruning configurable via the deployer spec.
	const enablePruning = true

	if err = r.applyWithApplySet(ctx, resource, deployer, objs, enablePruning); err != nil {
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
	controllerutil.AddFinalizer(deployer, resourceWatchFinalizer)

	// TODO: Status propagation of RGD status to deployer
	//       (see https://github.com/open-component-model/ocm-k8s-toolkit/issues/192)
	status.MarkReady(r.EventRecorder, deployer, "Applied version %s", resource.Status.Resource.Version)

	// we requeue the deployer after the requeue time specified in the resource.
	return ctrl.Result{RequeueAfter: resource.GetRequeueAfter()}, nil
}

func (r *Reconciler) DownloadResourceWithOCM(
	ctx context.Context,
	deployer *deliveryv1alpha1.Deployer,
	resource *deliveryv1alpha1.Resource,
) (objs []*unstructured.Unstructured, err error) {
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), deployer)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), deployer, deliveryv1alpha1.ConfigureContextFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get effective config: %w", err)
	}

	octx, session, err := r.OCMContextCache.GetSession(&ocm.GetSessionOptions{
		RepositorySpecification: resource.Status.Component.RepositorySpec,
		OCMConfigurations:       configs,
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), deployer, deliveryv1alpha1.ConfigureContextFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	spec, err := octx.RepositorySpecForConfig(resource.Status.Component.RepositorySpec.Raw, nil)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get repository spec: %w", err)
	}

	repo, err := session.LookupRepository(octx, spec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("invalid repository spec: %w", err)
	}

	cv, err := session.LookupComponentVersion(repo, resource.Status.Component.Component, resource.Status.Component.Version)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetComponentVersionFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get component version: %w", err)
	}

	// Take the resource reference from the status to ensure we are getting the exact same resource
	resourceSelector := rscsel.And(
		rscsel.Name(resource.Status.Resource.Name),
		rscsel.Version(resource.Status.Resource.Version),
		rscsel.ExtraIdentity(resource.Status.Resource.ExtraIdentity),
	)

	resourceAccesses, err := cv.SelectResources(resourceSelector)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get resource access: %w", err)
	}

	var resourceAccess ocmctx.ResourceAccess
	switch len(resourceAccesses) {
	case 0:
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, "resource not found in component version")
		return nil, fmt.Errorf("resource not found in component version")
	case 1:
		resourceAccess = resourceAccesses[0]
	default:
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, "multiple resources found in component version")
		return nil, fmt.Errorf("multiple resources found in component version")
	}

	if err := ocm.VerifyResource(resourceAccess, cv); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to verify resource: %w", err)
	}

	// Get the manifest and its digest. Compare the digest to the one in the resource to make
	// sure the resource is up to date.
	manifest, digests, err := r.getResource(cv, resourceAccess)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}
	defer func() {
		err = errors.Join(err, manifest.Close())
	}()

	// TODO: There is room for improvement here but this will be reworked when we migrate to ocm v2 either way
	// It is possible that the resource status does not have a digest as that digest is derived from the component
	// descriptor. If a digest is present in the resource status, we verify that it matches one of the digests
	// calculated for the resource.
	if resource.Status.Resource.Digest != nil {
		found := slices.ContainsFunc(digests, func(d ocmv1.DigestSpec) bool {
			return d.NormalisationAlgorithm == resource.Status.Resource.Digest.NormalisationAlgorithm &&
				d.HashAlgorithm == resource.Status.Resource.Digest.HashAlgorithm &&
				d.Value == resource.Status.Resource.Digest.Value
		})

		if !found {
			status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, "resource digest mismatch")

			return nil, fmt.Errorf("resource digest mismatch: none of %v matched with %s", digests, resource.Status.Resource.Digest)
		}
	}

	if objs, err = decodeObjectsFromManifest(manifest); err != nil {
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

// getResource returns the resource data as byte-slice and its digest.
func (r *Reconciler) getResource(cv ocmctx.ComponentVersionAccess, resourceAccess ocmctx.ResourceAccess) (io.ReadCloser, []ocmv1.DigestSpec, error) {
	octx := cv.GetContext()
	cd := cv.GetDescriptor()
	raw := &cd.Resources[cd.GetResourceIndex(resourceAccess.Meta())]

	if raw.Digest == nil {
		return nil, nil, errors.New("digest not found in resource access")
	}

	// Check if the resource is signature relevant and calculate digest of resource
	acc, err := octx.AccessSpecForSpec(raw.Access)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting access for resource: %w", err)
	}

	meth, err := acc.AccessMethod(cv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting access method: %w", err)
	}

	accessMethod, err := resourceAccess.AccessMethod()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create access method: %w", err)
	}

	bAcc := accessMethod.AsBlobAccess()

	meth = signing.NewRedirectedAccessMethod(meth, bAcc)
	resAccDigest := raw.Digest
	resAccDigestType := signing.DigesterType(resAccDigest)
	req := []ocmctx.DigesterType{resAccDigestType}

	registry := signingattr.Get(octx).HandlerRegistry()
	hasher := registry.GetHasher(resAccDigestType.HashAlgorithm)
	digest, err := octx.BlobDigesters().DetermineDigests(raw.Type, hasher, registry, meth, req...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed determining digest for resource: %w", err)
	}

	// Get actual resource data
	data, err := bAcc.Reader()
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting resource data: %w", err)
	}

	return data, digest, nil
}

// applyWithApplySet applies the resource objects using ApplySet for proper tracking and pruning.
// This method uses the ApplySet specification (KEP-3659) to manage sets of resources with automatic
// pruning of orphaned resources.
//
// The deployer object itself is used as the ApplySet parent, which means:
// - All deployed resources are labeled with applyset.k8s.io/part-of=<applyset-id>
// - The deployer carries annotations tracking the GroupKinds and namespaces of managed resources
// - Pruning automatically removes resources that were previously deployed but are no longer in the manifest
func (r *Reconciler) applyWithApplySet(ctx context.Context, resource *deliveryv1alpha1.Resource, deployer *deliveryv1alpha1.Deployer, objs []*unstructured.Unstructured, prune bool) error {
	logger := log.FromContext(ctx).WithValues("deployer", deployer.Name, "namespace", deployer.Namespace)

	// Use the deployer as the ApplySet parent
	// This allows us to track all resources deployed by this deployer
	applySetConfig := applyset.Config{
		ToolingID: applyset.ToolingID{
			Name:    deployerManager,
			Version: "v1alpha1",
		},
		FieldManager: fmt.Sprintf("%s/%s", deployerManager, deployer.UID),
		ToolLabels: map[string]string{
			// Include the standard managed-by label
			managedByLabel: deployerManager,
		},
		Concurrency: runtime.NumCPU(),
	}

	set, err := applyset.New(ctx, deployer, r.Client, r.resourceRESTMapper, applySetConfig)
	if err != nil {
		return fmt.Errorf("failed to create ApplySet: %w", err)
	}

	logger.Info("adding objects to ApplySet", "count", len(objs))

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

		// Add to the ApplySet
		if _, err := set.Add(ctx, obj); err != nil {
			return fmt.Errorf("failed to add object %s/%s to ApplySet: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	}

	// Apply all objects (and prune if requested)
	logger.Info("applying ApplySet", "prune", prune)
	result, err := set.Apply(ctx, prune)
	if err != nil {
		return fmt.Errorf("failed to apply ApplySet: %w", err)
	}

	// Log results
	logger.Info("ApplySet operation complete",
		"applied", len(result.Applied),
		"pruned", len(result.Pruned),
		"errors", len(result.Errors))

	if len(result.Errors) > 0 {
		return fmt.Errorf("ApplySet completed with errors: %v", result.Errors)
	}

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
