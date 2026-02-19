package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	ocmcel "ocm.software/open-component-model/kubernetes/controller/internal/cel"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

type Reconciler struct {
	*ocm.BaseReconciler

	// Resolver provides repository resolution and caching for resource reconciliation.
	// It ensures that repository access is efficient and consistent during reconciliation operations.
	Resolver *resolution.Resolver

	// PluginManager manages plugins for resource operations.
	// It enables dynamic loading and execution of plugins required for resource access.
	PluginManager *manager.PluginManager
}

var _ ocm.Reconciler = (*Reconciler)(nil)

var deployerIndex = "Resource.spec.resourceRef"

func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, concurrency int) error {
	// Build index for resources that reference a component to make sure that we get notified when a component changes.
	const fieldName = "spec.componentRef.name"
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Resource{}, fieldName, func(obj client.Object) []string {
		resource, ok := obj.(*v1alpha1.Resource)
		if !ok {
			return nil
		}

		return []string{resource.Spec.ComponentRef.Name}
	}); err != nil {
		return err
	}

	// This index is required to get all deployers that reference a resource. This is required to make sure that when
	// deleting the resource, no deployer exists anymore that references that resource.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&v1alpha1.Deployer{},
		deployerIndex,
		func(obj client.Object) []string {
			deployer, ok := obj.(*v1alpha1.Deployer)
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
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	// event source from resolver's worker pool to get notified when resolutions complete
	eventSource := workerpool.NewEventSource(r.Resolver.WorkerPool())

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Resource{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(eventSource).
		// Watch for component-events that are referenced by resources
		Watches(
			// Watch for changes to components that are referenced by a resource.
			&v1alpha1.Component{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				component, ok := obj.(*v1alpha1.Component)
				if !ok {
					return []reconcile.Request{}
				}

				// Get list of resources that reference the component
				list := &v1alpha1.ResourceList{}
				if err := r.List(ctx, list, client.MatchingFields{fieldName: component.GetName()}); err != nil {
					return []reconcile.Request{}
				}

				// For every resource that references the component create a reconciliation request for that resource
				requests := make([]reconcile.Request, 0, len(list.Items))
				for _, resource := range list.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: k8stypes.NamespacedName{
							Namespace: resource.GetNamespace(),
							Name:      resource.GetName(),
						},
					})
				}

				return requests
			})).
		Watches(
			// Ensure to reconcile the resource when a deployer changes that references this resource. We want to
			// reconcile because the resource-finalizer makes sure that the resource is only deleted when
			// it is not referenced by any deployer anymore. So, when the resource is already marked for deletion, we
			// want to get notified about deployer changes (e.g. deletion) to remove the resource-finalizer
			// respectively.
			&v1alpha1.Deployer{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				deployer, ok := obj.(*v1alpha1.Deployer)
				if !ok {
					return []reconcile.Request{}
				}

				resource := &v1alpha1.Resource{}
				if err := r.Get(ctx, client.ObjectKey{
					Namespace: deployer.Spec.ResourceRef.Namespace,
					Name:      deployer.Spec.ResourceRef.Name,
				}, resource); err != nil {
					return []reconcile.Request{}
				}

				// Only reconcile if the resource is marked for deletion
				if resource.GetDeletionTimestamp().IsZero() {
					return []reconcile.Request{}
				}

				return []reconcile.Request{
					{NamespacedName: k8stypes.NamespacedName{
						Namespace: resource.GetNamespace(),
						Name:      resource.GetName(),
					}},
				}
			})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: concurrency,
		}).
		Complete(r)
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=resources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=resources/status,verbs=get;update;patch

//nolint:cyclop,funlen,gocognit,maintidx // we do not want to cut the function at arbitrary points
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("starting reconciliation")

	resource := &v1alpha1.Resource{}
	if err := r.Get(ctx, req.NamespacedName, resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper := patch.NewSerialPatcher(resource, r.Client)
	defer func(ctx context.Context) {
		err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, resource, r.EventRecorder, resource.GetRequeueAfter(), err))
	}(ctx)

	logger.Info("preparing reconciling resource")
	if resource.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	if !resource.GetDeletionTimestamp().IsZero() {
		logger.Info("resource is marked for deletion, attempting cleanup")
		// The resource should only be deleted if no deployer exists that references that resource.
		deployerList := &v1alpha1.DeployerList{}
		if err := r.List(ctx, deployerList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				deployerIndex,
				client.ObjectKeyFromObject(resource).String(),
			),
		}); err != nil {
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.DeletionFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to list deployers: %w", err)
		}

		if len(deployerList.Items) > 0 {
			var names []string
			for _, deployer := range deployerList.Items {
				names = append(names, deployer.Name)
			}

			msg := fmt.Sprintf(
				"resource cannot be removed as deployers are still referencing it: %s",
				strings.Join(names, ","),
			)
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.DeletionFailedReason, msg)

			return ctrl.Result{}, errors.New(msg)
		}

		if updated := controllerutil.RemoveFinalizer(resource, v1alpha1.ResourceFinalizer); updated {
			if err := r.Update(ctx, resource); err != nil {
				status.MarkNotReady(r.EventRecorder, resource, v1alpha1.DeletionFailedReason, err.Error())

				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}

			return ctrl.Result{}, nil
		}

		status.MarkNotReady(
			r.EventRecorder,
			resource,
			v1alpha1.DeletionFailedReason,
			"resource is being deleted and still has existing finalizers",
		)

		return ctrl.Result{}, nil
	}

	if updated := controllerutil.AddFinalizer(resource, v1alpha1.ResourceFinalizer); updated {
		if err := r.Update(ctx, resource); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: resource.GetNamespace(),
		Name:      resource.Spec.ComponentRef.Name,
	})
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.ResourceIsNotAvailable, err.Error())

		if errors.Is(err, util.NotReadyError{}) || errors.Is(err, util.DeletionError{}) {
			logger.Info("component is not available", "error", err)

			// return no requeue as we watch the object for changes anyway
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get ready component: %w", err)
	}

	logger.Info("reconciling resource")
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), resource, component)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to configure context: %w", err)
	}

	repoSpec := &runtime.Raw{}
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Decode(
		bytes.NewReader(component.Status.Component.RepositorySpec.Raw), repoSpec); err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	cacheBackedRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
		RepositorySpec:    repoSpec,
		OCMConfigurations: configs,
		Namespace:         resource.GetNamespace(),
		SigningRegistry:   r.PluginManager.SigningRegistry,
		RequesterFunc: func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: resource.GetNamespace(),
					Name:      resource.GetName(),
				},
			}
		},
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.GetRepositoryFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	// Add verifications from the component to the cache-backed repository to make sure they are included in the
	// cache key and used for verification.
	verifications, err := ocm.GetVerifications(ctx, r.Client, component)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get verifications: %w", err)
	}
	cacheBackedRepo.Verifications = verifications

	referencedDescriptor, err := cacheBackedRepo.GetComponentVersion(ctx,
		component.Status.Component.Component,
		component.Status.Component.Version)
	if err != nil {
		switch {
		case errors.Is(err, workerpool.ErrResolutionInProgress):
			// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.ResolutionInProgress, err.Error())
			logger.Info("component version resolution in progress, waiting for event notification",
				"component", component.Status.Component.Component,
				"version", component.Status.Component.Version)

			return ctrl.Result{}, nil
		case errors.Is(err, workerpool.ErrNotSafelyDigestible):
			// Ignore error, but log event
			event.New(r.EventRecorder, component, nil, eventv1.EventSeverityInfo, err.Error())
		default:
			status.MarkNotReady(r.EventRecorder, component, v1alpha1.GetComponentVersionFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to get component version: %w", err)
		}
	}

	startRetrievingResource := time.Now()
	logger.V(1).Info("resolving reference path", "referencePath", resource.Spec.Resource.ByReference.ReferencePath)
	resourceDescriptor, resourceRepoSpec, err := r.resolveReferencePath(
		ctx,
		referencedDescriptor,
		repoSpec,
		resource.Spec.Resource.ByReference.ReferencePath,
		configs,
		workerpool.RequesterInfo{
			NamespacedName: k8stypes.NamespacedName{
				Namespace: resource.GetNamespace(),
				Name:      resource.GetName(),
			},
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, workerpool.ErrResolutionInProgress):
			// Resolution is in progress, the controller will be re-triggered via event source when resolution completes
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.ResolutionInProgress, err.Error())
			logger.Info("reference path resolution in progress, waiting for event notification")

			return ctrl.Result{}, nil
		case errors.Is(err, workerpool.ErrNotSafelyDigestible):
			// Ignore error, but log event
			event.New(r.EventRecorder, resource, nil, eventv1.EventSeverityInfo, err.Error())
		default:
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetOCMResourceFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed to resolve reference path: %w", err)
		}
	}

	resourceIdentity := resource.Spec.Resource.ByReference.Resource
	var matchedResource *descriptor.Resource
	for i, res := range resourceDescriptor.Component.Resources {
		resIdentity := res.ToIdentity()
		if resourceIdentity.Match(resIdentity, identityFunc()) {
			matchedResource = &resourceDescriptor.Component.Resources[i]
			break
		}
	}

	if matchedResource == nil {
		err := fmt.Errorf("resource with identity %v not found in component %s:%s",
			resourceIdentity, resourceDescriptor.Component.Name, resourceDescriptor.Component.Version)
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetOCMResourceFailedReason, err.Error())

		return ctrl.Result{}, err
	}

	if !resource.Spec.SkipVerify {
		logger.V(1).Info("verifying resource")

		cfg, err := configuration.LoadConfigurations(ctx, r.Client, resource.GetNamespace(), configs)
		if err != nil {
			status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetOCMResourceFailedReason, err.Error())

			return ctrl.Result{}, fmt.Errorf("failed getting configs: %w", err)
		}

		matchedResource, err = ocm.VerifyResource(ctx, r.PluginManager, matchedResource, cfg)
		if err != nil {
			if errors.Is(err, ocm.ErrPluginNotFound) {
				// TODO(@frewilhelm): For now we skip resource types that do not have a digest processor plugin.
				//                    We need to adjust this when the plugins are available
				logger.V(1).Info("skipping resource verification as no suitable plugin was found")
			} else {
				status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetOCMResourceFailedReason, err.Error())

				return ctrl.Result{}, fmt.Errorf("failed to verify resource: %w", err)
			}
		}
	} else {
		logger.V(1).Info("skip verifying resource")
	}

	logger.V(1).Info("retrieved resource", "component", fmt.Sprintf("%s:%s", resourceDescriptor.Component.Name, resourceDescriptor.Component.Version),
		"resource", matchedResource.Name, "duration", time.Since(startRetrievingResource))

	resourceRepoSpecData, err := json.Marshal(resourceRepoSpec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.MarshalFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to marshal final repository spec: %w", err)
	}

	if err = setResourceStatus(ctx, configs, resource, matchedResource, &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: resourceRepoSpecData},
		Component:      resourceDescriptor.Component.Name,
		Version:        resourceDescriptor.Component.Version,
	}); err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.StatusSetFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to set resource status: %w", err)
	}

	status.MarkReady(r.EventRecorder, resource, "Applied version %s", matchedResource.Version)

	return ctrl.Result{RequeueAfter: resource.GetRequeueAfter()}, nil
}

// resolveReferencePath walks a reference path from a parent component version to a final component version.
// It returns the final descriptor and repository spec.
func (r *Reconciler) resolveReferencePath(
	ctx context.Context,
	parentDesc *descriptor.Descriptor,
	parentRepoSpec runtime.Typed,
	referencePath []runtime.Identity,
	configs []v1alpha1.OCMConfiguration,
	reqInfo workerpool.RequesterInfo,
) (*descriptor.Descriptor, runtime.Typed, error) {
	logger := log.FromContext(ctx)

	if len(referencePath) == 0 {
		return parentDesc, parentRepoSpec, nil
	}

	currentDesc := parentDesc
	currentRepoSpec := parentRepoSpec
	// referenceDigestFromParent stores the component reference digest spec from the parent component.
	var referenceDigestFromParent *v2.Digest

	for i, refIdentity := range referencePath {
		logger.V(1).Info("resolving reference", "step", i+1, "identity", refIdentity)

		var matchedRef *descriptor.Reference
		for j, ref := range currentDesc.Component.References {
			refIdent := ref.ToIdentity()
			if refIdentity.Match(refIdent, identityFunc()) {
				matchedRef = &currentDesc.Component.References[j]
				break
			}
		}

		if matchedRef == nil {
			return nil, nil, fmt.Errorf("component reference with identity %v not found in component %s:%s at reference path step %d",
				refIdentity, currentDesc.Component.Name, currentDesc.Component.Version, i+1)
		}

		refRepo, err := r.Resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
			RepositorySpec:    currentRepoSpec,
			OCMConfigurations: configs,
			Namespace:         reqInfo.NamespacedName.Namespace,
			SigningRegistry:   r.PluginManager.SigningRegistry,
			RequesterFunc: func() workerpool.RequesterInfo {
				return reqInfo
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create cache-backed repository for reference: %w", err)
		}

		// TODO(@frewilhelm): Are we sure that only verified component versions have a digest spec in their references?
		var referenceDigest *v2.Digest
		if matchedRef.Digest.Value != "" {
			referenceDigest = &v2.Digest{
				HashAlgorithm:          matchedRef.Digest.HashAlgorithm,
				Value:                  matchedRef.Digest.Value,
				NormalisationAlgorithm: matchedRef.Digest.NormalisationAlgorithm,
			}

			// Only set component reference digest if the matched reference is from the (original) parent component version (i == 0)
			if currentDesc.Component.Name == parentDesc.Component.Name &&
				currentDesc.Component.Version == parentDesc.Component.Version {
				referenceDigestFromParent = referenceDigest
			}
		}

		// If the parent component contained a digest spec for the component reference, we set it for the
		// cache-backed repository, so it is used for the cache-key creation.
		// We need to use the digest spec from the parent component descriptor as this will be the only information
		// available in the deployer controller to retrieve the respective cache-key to the verified/integrity-checked
		// component version.
		if referenceDigestFromParent != nil {
			refRepo.Digest = referenceDigestFromParent
		}

		refDesc, err := refRepo.GetComponentVersion(ctx, matchedRef.Component, matchedRef.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get referenced component version %s:%s: %w",
				matchedRef.Component, matchedRef.Version, err)
		}

		// Digest integrity check for the referenced component version if reference contains a digest
		if matchedRef.Digest.Value != "" {
			logger.Info("verifying digest for referenced component version", "parent component",
				fmt.Sprintf("%s:%s", matchedRef.Component, matchedRef.Version), "child component",
				fmt.Sprintf("%s:%s", refDesc.Component.Name, refDesc.Component.Version))

			childDigest, err := signing.GenerateDigest(ctx, refDesc, slog.New(logr.ToSlogHandler(logger)),
				matchedRef.Digest.NormalisationAlgorithm, matchedRef.Digest.HashAlgorithm)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to generate digest for referenced component version %s:%s: %w",
					matchedRef.Component, matchedRef.Version, err)
			}

			if matchedRef.Digest.Value != childDigest.Value {
				return nil, nil, fmt.Errorf("digest mismatch for referenced component version %s:%s: expected %s but got %s",
					matchedRef.Component, matchedRef.Version, matchedRef.Digest.Value, childDigest.Value)
			}

			logger.Info("digest successfully verified", "parent component",
				fmt.Sprintf("%s:%s", matchedRef.Component, matchedRef.Version), "child component",
				fmt.Sprintf("%s:%s", refDesc.Component.Name, refDesc.Component.Version))
		}

		currentDesc = refDesc
	}

	return currentDesc, currentRepoSpec, nil
}

// setResourceStatus updates the resource status with all required information.
func setResourceStatus(
	ctx context.Context,
	configs []v1alpha1.OCMConfiguration,
	resource *v1alpha1.Resource,
	res *descriptor.Resource,
	component *v1alpha1.ComponentInfo,
) error {
	log.FromContext(ctx).V(1).Info("updating resource status")

	info, err := buildResourceInfo(res)
	if err != nil {
		return fmt.Errorf("building resource info: %w", err)
	}
	resource.Status.Resource = info

	if err := computeAdditionalStatusFields(ctx, res, resource); err != nil {
		return fmt.Errorf("evaluating additional status fields: %w", err)
	}

	resource.Status.EffectiveOCMConfig = configs
	resource.Status.Component = component

	return nil
}

// buildResourceInfo constructs a ResourceInfo from a descriptor resource.
func buildResourceInfo(res *descriptor.Resource) (*v1alpha1.ResourceInfo, error) {
	raw, err := json.Marshal(res.Access)
	if err != nil {
		return nil, fmt.Errorf("marshaling access spec: %w", err)
	}

	labels, err := convertLabels(res.Labels)
	if err != nil {
		return nil, fmt.Errorf("converting labels: %w", err)
	}

	return &v1alpha1.ResourceInfo{
		Name:          res.Name,
		Type:          res.Type,
		Version:       res.Version,
		ExtraIdentity: res.ExtraIdentity,
		Access:        apiextensionsv1.JSON{Raw: raw},
		Digest:        descriptor.ConvertToV2Digest(res.Digest),
		Labels:        labels,
	}, nil
}

// convertLabels maps descriptor labels to API Label objects.
func convertLabels(in []descriptor.Label) ([]v1alpha1.Label, error) {
	out := make([]v1alpha1.Label, len(in))
	for i, l := range in {
		valueBytes, err := json.Marshal(l.Value)
		if err != nil {
			return nil, fmt.Errorf("marshaling label %q value: %w", l.Name, err)
		}
		out[i] = v1alpha1.Label{
			Name:    l.Name,
			Value:   apiextensionsv1.JSON{Raw: valueBytes},
			Version: l.Version,
			Signing: l.Signing,
		}
	}

	return out, nil
}

// computeAdditionalStatusFields compiles and evaluates CEL expressions for additional fields.
func computeAdditionalStatusFields(
	ctx context.Context,
	res *descriptor.Resource,
	resource *v1alpha1.Resource,
) error {
	env, err := ocmcel.BaseEnv()
	if err != nil {
		return fmt.Errorf("getting base CEL env: %w", err)
	}
	env, err = env.Extend(
		cel.Variable("resource", cel.DynType),
	)
	if err != nil {
		return fmt.Errorf("extending CEL env: %w", err)
	}

	resV2, err := descriptor.ConvertToV2Resource(runtime.NewScheme(runtime.WithAllowUnknown()), res)
	if err != nil {
		return fmt.Errorf("converting resource to v2: %w", err)
	}

	resourceMap, err := toGenericMapViaJSON(resV2)
	if err != nil {
		return fmt.Errorf("preparing CEL variables: %w", err)
	}

	statusFields := resource.Spec.AdditionalStatusFields
	resource.Status.Additional = make(map[string]apiextensionsv1.JSON, len(statusFields))

	for name, expr := range statusFields {
		ast, issues := env.Compile(expr)
		if issues.Err() != nil {
			return fmt.Errorf("compiling CEL %q: %w", name, issues.Err())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return fmt.Errorf("building CEL program %q: %w", name, err)
		}
		val, _, err := prog.ContextEval(ctx, map[string]any{"resource": resourceMap})
		if err != nil {
			return fmt.Errorf("evaluating CEL %q: %w", name, err)
		}
		raw, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshaling CEL result %q: %w", name, err)
		}
		resource.Status.Additional[name] = apiextensionsv1.JSON{Raw: raw}
	}

	return nil
}

// toGenericMapViaJSON marshals and unmarshals a struct into a generic map representation through JSON tags.
func toGenericMapViaJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	return m, nil
}

// identityFunc is a custom identity matching function that ignores the "version" field if it is not set.
func identityFunc() runtime.IdentityMatchingChainFn {
	return func(i, o runtime.Identity) bool {
		version, ok := i["version"]
		if !ok || version == "" {
			delete(o, "version")
		}
		return runtime.IdentityEqual(i, o)
	}
}
