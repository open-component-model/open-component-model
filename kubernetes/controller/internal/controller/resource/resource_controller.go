package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/google/cel-go/cel"
	"github.com/mandelsoft/goutils/sliceutils"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/compdesc"
	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	ocmcel "ocm.software/open-component-model/kubernetes/controller/internal/cel"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

type Reconciler struct {
	*ocm.BaseReconciler

	CELEnvironment  *cel.Env
	OCMContextCache *ocm.ContextCache
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Resource{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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

//nolint:cyclop,funlen,gocognit // we do not want to cut the function at arbitrary points
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
	configs, err := ocm.GetEffectiveConfig(ctx, r.GetClient(), resource)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to configure context: %w", err)
	}

	octx, session, err := r.OCMContextCache.GetSession(&ocm.GetSessionOptions{
		RepositorySpecification: component.Status.Component.RepositorySpec,
		OCMConfigurations:       configs,
		VerificationProvider:    component,
	})
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.ConfigureContextFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get session: %w", err)
	}
	logger = logger.WithValues("ocmContext", octx.GetId())

	spec, err := octx.RepositorySpecForConfig(component.Status.Component.RepositorySpec.Raw, nil)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get repository spec: %w", err)
	}

	repo, err := session.LookupRepository(octx, spec)
	if err != nil {
		status.MarkNotReady(r.GetEventRecorder(), resource, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to lookup repository: %w", err)
	}

	cv, err := session.LookupComponentVersion(repo, component.Status.Component.Component, component.Status.Component.Version)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to lookup component version: %w", err)
	}

	sessionResolver := ocm.NewSessionResolver(octx, session)

	_, err = ocm.VerifyComponentVersionAndListDescriptors(ctx, cv, sessionResolver, sliceutils.Transform(component.Spec.Verify, func(verify v1alpha1.Verification) string {
		return verify.Signature
	}))
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetComponentVersionFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to list verified descriptors: %w", err)
	}

	resourceReference := v1.ResourceReference{
		Resource:      resource.Spec.Resource.ByReference.Resource,
		ReferencePath: resource.Spec.Resource.ByReference.ReferencePath,
	}

	startRetrievingResource := time.Now()
	logger.V(1).Info("retrieving resource", "component", fmt.Sprintf("%s:%s", cv.GetName(), cv.GetVersion()), "reference", resourceReference, "duration", time.Since(startRetrievingResource))
	resourceAccess, resourceCV, err := ocm.GetResourceAccessForComponentVersion(ctx, cv, resourceReference, sessionResolver, resource.Spec.SkipVerify)
	logger.V(1).Info("retrieved resource", "component", fmt.Sprintf("%s:%s", cv.GetName(), cv.GetVersion()), "reference", resourceReference, "duration", time.Since(startRetrievingResource))
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.GetOCMResourceFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to get resource access: %w", err)
	}

	// Get repository spec of actual component descriptor of the referenced resource
	resCompVersRepoSpec := resourceCV.Repository().GetSpecification()
	resCompVersRepoSpecData, err := json.Marshal(resCompVersRepoSpec)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.MarshalFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to marshal resource spec: %w", err)
	}

	// Update status
	if err = setResourceStatus(ctx, configs, resource, resourceAccess, &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: resCompVersRepoSpecData},
		Component:      resourceCV.GetName(),
		Version:        resourceCV.GetVersion(),
	}); err != nil {
		status.MarkNotReady(r.EventRecorder, resource, v1alpha1.StatusSetFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("failed to set resource status: %w", err)
	}

	status.MarkReady(r.EventRecorder, resource, "Applied version %s", resourceAccess.Meta().GetVersion())

	return ctrl.Result{RequeueAfter: resource.GetRequeueAfter()}, nil
}

// setResourceStatus updates the resource status with all required information.
func setResourceStatus(
	ctx context.Context,
	configs []v1alpha1.OCMConfiguration,
	resource *v1alpha1.Resource,
	access ocmctx.ResourceAccess,
	component *v1alpha1.ComponentInfo,
) error {
	log.FromContext(ctx).V(1).Info("updating resource status")

	spec, err := access.Access()
	if err != nil {
		return fmt.Errorf("getting access spec: %w", err)
	}

	info, err := buildResourceInfo(access, spec)
	if err != nil {
		return fmt.Errorf("building resource info: %w", err)
	}
	resource.Status.Resource = info

	if err := computeAdditionalStatusFields(ctx, compdesc.Resource{
		ResourceMeta: *access.Meta(),
		Access:       spec,
	}, resource); err != nil {
		return fmt.Errorf("evaluating additional status fields: %w", err)
	}

	resource.Status.EffectiveOCMConfig = configs
	resource.Status.Component = component

	return nil
}

// buildResourceInfo constructs a ResourceInfo from the access metadata and spec.
func buildResourceInfo(
	access ocmctx.ResourceAccess,
	spec any,
) (*v1alpha1.ResourceInfo, error) {
	meta := access.Meta()
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshaling access spec: %w", err)
	}

	labels := convertLabels(meta.Labels)

	return &v1alpha1.ResourceInfo{
		Name:          meta.Name,
		Type:          meta.Type,
		Version:       meta.Version,
		ExtraIdentity: meta.ExtraIdentity,
		Access:        apiextensionsv1.JSON{Raw: raw},
		Digest:        meta.Digest.String(),
		Labels:        labels,
	}, nil
}

// convertLabels maps metadata labels to API Label objects.
func convertLabels(in compdesc.Labels) []v1alpha1.Label {
	out := make([]v1alpha1.Label, len(in))
	for i, l := range in {
		out[i] = v1alpha1.Label{
			Name:    l.Name,
			Value:   apiextensionsv1.JSON{Raw: l.Value},
			Version: l.Version,
			Signing: l.Signing,
		}
		if l.Merge != nil {
			out[i].Merge = &v1alpha1.MergeAlgorithmSpecification{
				Algorithm: l.Merge.Algorithm,
				Config:    apiextensionsv1.JSON{Raw: l.Merge.Config},
			}
		}
	}

	return out
}

// computeAdditionalStatusFields compiles and evaluates CEL expressions for additional fields.
func computeAdditionalStatusFields(
	ctx context.Context,
	res compdesc.Resource,
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

	resourceMap, err := toGenericMapViaJSON(res)
	if err != nil {
		return fmt.Errorf("preparing CEL variables: %w", err)
	}

	fields := resource.Spec.AdditionalStatusFields
	resource.Status.Additional = make(map[string]apiextensionsv1.JSON, len(fields))

	for name, expr := range fields {
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
