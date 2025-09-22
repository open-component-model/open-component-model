package deployer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"slices"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	ocmctx "ocm.software/ocm/api/ocm"
	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/extensions/attrs/signingattr"
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
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/cache"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/deployer/dynamic"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
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

	DownloadCache cache.DigestObjectCache[string, []*unstructured.Unstructured]

	OCMContextCache *ocm.ContextCache
}

var _ ocm.Reconciler = (*Reconciler)(nil)

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=deployers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kro.run,resources=resourcegraphdefinitions,verbs=list;watch;create;update;patch

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

	// Download the resource
	key := resource.Status.Resource.Digest

	objs, err := r.DownloadCache.Load(key, func() ([]*unstructured.Unstructured, error) {
		return r.DownloadResourceWithOCM(ctx, deployer, resource)
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to download resource from OCM or retrieve it from the cache: %w", err)
	}

	if err = r.applyConcurrently(ctx, resource, deployer, objs); err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.ApplyFailed, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to apply resources: %w", err)
	}

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

	resourceReference := v1.ResourceReference{
		Resource:      resource.Spec.Resource.ByReference.Resource,
		ReferencePath: resource.Spec.Resource.ByReference.ReferencePath,
	}

	resourceAccess, _, err := ocm.GetResourceAccessForComponentVersion(ctx, cv, resourceReference, ocm.NewSessionResolver(octx, session), resource.Spec.SkipVerify)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get resource access: %w", err)
	}

	// Get the manifest and its digest. Compare the digest to the one in the resource to make
	// sure the resource is up to date.
	manifest, digest, err := r.getResource(cv, resourceAccess)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, err.Error())

		return nil, fmt.Errorf("failed to get  manifest: %w", err)
	}
	defer func() {
		err = errors.Join(err, manifest.Close())
	}()

	if resource.Status.Resource.Digest != digest {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.GetOCMResourceFailedReason, "resource digest mismatch")

		return nil, fmt.Errorf("resource digest mismatch: expected %s, got %s", resource.Status.Resource.Digest, digest)
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
func (r *Reconciler) getResource(cv ocmctx.ComponentVersionAccess, resourceAccess ocmctx.ResourceAccess) (io.ReadCloser, string, error) {
	octx := cv.GetContext()
	cd := cv.GetDescriptor()
	raw := &cd.Resources[cd.GetResourceIndex(resourceAccess.Meta())]

	if raw.Digest == nil {
		return nil, "", errors.New("digest not found in resource access")
	}

	// Check if the resource is signature relevant and calculate digest of resource
	acc, err := octx.AccessSpecForSpec(raw.Access)
	if err != nil {
		return nil, "", fmt.Errorf("failed getting access for resource: %w", err)
	}

	meth, err := acc.AccessMethod(cv)
	if err != nil {
		return nil, "", fmt.Errorf("failed getting access method: %w", err)
	}

	accessMethod, err := resourceAccess.AccessMethod()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create access method: %w", err)
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
		return nil, "", fmt.Errorf("failed determining digest for resource: %w", err)
	}

	// Get actual resource data
	data, err := bAcc.Reader()
	if err != nil {
		return nil, "", fmt.Errorf("failed getting resource data: %w", err)
	}

	return data, digest[0].String(), nil
}

var digestSpecStringPattern = regexp.MustCompile(`^(?P<algo>[\w\-]+):(?P<digest>[a-fA-F0-9]+)\[(?P<norm>[\w\/]+)\]$`)

// TODO(jakobmoellerdev): currently digests are stored as strings in resource status, we should really consider storing them natively...
func digestSpec(s string) (v1.DigestSpec, error) {
	matches := digestSpecStringPattern.FindStringSubmatch(s)
	if expectedMatches := 4; len(matches) != expectedMatches {
		return v1.DigestSpec{}, fmt.Errorf("invalid digest spec format: %s", s)
	}

	digestSpec := v1.DigestSpec{}
	for i, name := range digestSpecStringPattern.SubexpNames() {
		switch name {
		case "algo":
			digestSpec.HashAlgorithm = matches[i]
		case "digest":
			digestSpec.Value = matches[i]
		case "norm":
			digestSpec.NormalisationAlgorithm = matches[i]
		}
	}

	return digestSpec, nil
}

// applyConcurrently applies the resource objects to the cluster concurrently.
//
// See Apply for more details on how the objects are applied.
func (r *Reconciler) applyConcurrently(ctx context.Context, resource *deliveryv1alpha1.Resource, deployer *deliveryv1alpha1.Deployer, objs []*unstructured.Unstructured) error {
	if len(objs) > 1 {
		// TODO(jakobmoellerdev): remove once https://github.com/open-component-model/ocm-k8s-toolkit/issues/273#issue-3201709052
		//  is implemented in the deployer controller. We need proper apply detection so we can support pruning diffs.
		//  Otherwise we can orphan resources.
		msg := "multiple objects found in manifest," +
			"the current deployer implementation does not officially support this yet," +
			"and will not prune diffs properly."
		event.New(r, deployer, nil, eventv1.EventSeverityInfo, msg)
		log.FromContext(ctx).Info(msg)
	}

	eg, egctx := errgroup.WithContext(ctx)

	for i := range objs {
		eg.Go(func() error {
			//nolint:forcetypeassert // we know that objs[i] is a client.Object because we just cloned it
			obj := objs[i].DeepCopyObject().(*unstructured.Unstructured)

			return r.apply(egctx, resource, deployer, obj)
		})
	}

	return eg.Wait()
}

// apply applies the object to the cluster using Server-Side Apply. It sets the controller reference on the object
// and patches it with the FieldManager set to the deployer UID. It also updates the deployer status with the
// applied object reference.
func (r *Reconciler) apply(ctx context.Context, resource *deliveryv1alpha1.Resource, deployer *deliveryv1alpha1.Deployer, obj *unstructured.Unstructured) error {
	setOwnershipLabels(obj, resource, deployer)
	setOwnershipAnnotations(obj, resource)
	if err := controllerutil.SetControllerReference(deployer, obj, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on object: %w", err)
	}

	applyConfig := client.ApplyConfigurationFromUnstructured(obj)
	if err := r.GetClient().Apply(ctx, applyConfig, &client.ApplyOptions{
		Force:        ptr.To(true),
		FieldManager: fmt.Sprintf("%s/%s", deployerManager, deployer.UID),
	}); err != nil {
		return fmt.Errorf("failed to apply object: %w", err)
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
