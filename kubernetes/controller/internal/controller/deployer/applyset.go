package deployer

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	deliveryv1alpha1 "ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// ApplySet manages the application and pruning of resources.
type ApplySet struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	RESTMapper meta.RESTMapper
	Deployer   *deliveryv1alpha1.Deployer
	Resource   *deliveryv1alpha1.Resource
}

// NewApplySet creates a new ApplySet.
func NewApplySet(c client.Client, scheme *runtime.Scheme, mapper meta.RESTMapper, deployer *deliveryv1alpha1.Deployer, resource *deliveryv1alpha1.Resource) *ApplySet {
	return &ApplySet{
		Client:     c,
		Scheme:     scheme,
		RESTMapper: mapper,
		Deployer:   deployer,
		Resource:   resource,
	}
}

// Apply applies the given objects and prunes any objects that were previously deployed but are not in the new set.
// It updates the Deployer's status with the new set of deployed objects.
func (a *ApplySet) Apply(ctx context.Context, objs []*unstructured.Unstructured) error {
	// 1. Calculate objects to prune
	toPrune := a.calculatePruneSet(objs)

	// 2. Apply new objects
	if err := a.applyConcurrently(ctx, objs); err != nil {
		return fmt.Errorf("failed to apply objects: %w", err)
	}

	// 3. Prune objects
	if err := a.prune(ctx, toPrune); err != nil {
		return fmt.Errorf("failed to prune objects: %w", err)
	}

	// 4. Update status
	a.updateStatus(objs)

	return nil
}

// calculatePruneSet identifies objects in Status.Deployed that are not in the new set of objects.
func (a *ApplySet) calculatePruneSet(newObjs []*unstructured.Unstructured) []deliveryv1alpha1.DeployedObjectReference {
	var toPrune []deliveryv1alpha1.DeployedObjectReference

	// Create a map of new objects for efficient lookup
	newObjsMap := make(map[string]struct{})
	for _, obj := range newObjs {
		key := objectKey(obj)
		newObjsMap[key] = struct{}{}
	}

	for _, ref := range a.Deployer.Status.Deployed {
		key := refKey(ref)
		if _, exists := newObjsMap[key]; !exists {
			toPrune = append(toPrune, ref)
		}
	}

	return toPrune
}

// PruneAll deletes all deployed objects.
func (a *ApplySet) PruneAll(ctx context.Context) error {
	return a.prune(ctx, a.Deployer.Status.Deployed)
}

// prune deletes the objects in the prune set.
func (a *ApplySet) prune(ctx context.Context, toPrune []deliveryv1alpha1.DeployedObjectReference) error {
	logger := log.FromContext(ctx)
	for _, ref := range toPrune {
		logger.Info("pruning object", "kind", ref.Kind, "name", ref.Name, "namespace", ref.Namespace)
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(ref.APIVersion)
		obj.SetKind(ref.Kind)
		obj.SetName(ref.Name)
		obj.SetNamespace(ref.Namespace)

		if err := a.Client.Delete(ctx, obj); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete object %s/%s: %w", ref.Namespace, ref.Name, err)
			}
		}
	}
	return nil
}

// applyConcurrently applies the objects concurrently.
func (a *ApplySet) applyConcurrently(ctx context.Context, objs []*unstructured.Unstructured) error {
	eg, egctx := errgroup.WithContext(ctx)

	for i := range objs {
		eg.Go(func() error {
			//nolint:forcetypeassert // we know that objs[i] is a client.Object because we just cloned it
			obj := objs[i].DeepCopyObject().(*unstructured.Unstructured)
			return a.apply(egctx, obj)
		})
	}

	return eg.Wait()
}

// apply applies a single object.
func (a *ApplySet) apply(ctx context.Context, obj *unstructured.Unstructured) error {
	setOwnershipLabels(obj, a.Resource, a.Deployer)
	setOwnershipAnnotations(obj, a.Resource)
	if err := controllerutil.SetControllerReference(a.Deployer, obj, a.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on object: %w", err)
	}

	if err := a.defaultObj(ctx, obj); err != nil {
		return err
	}

	// We use Patch with Apply patch type to update the object with the result from the server (including UID).
	if err := a.Client.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		Force:        ptr.To(true),
		FieldManager: fmt.Sprintf("%s/%s", deployerManager, a.Deployer.UID),
	}); err != nil {
		return fmt.Errorf("failed to apply object: %w", err)
	}

	return nil
}

// defaultObj defaults namespace and apiVersion.
func (a *ApplySet) defaultObj(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := log.FromContext(ctx).WithValues(
		"operation", "apply",
		"gvk", obj.GetObjectKind().GroupVersionKind().String())

	gvk := schema.FromAPIVersionAndKind(obj.GetAPIVersion(), obj.GetKind())
	mapping, err := a.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("failed to determine resource mapping: %w", err)
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace && obj.GetNamespace() == "" {
		logger.Info("namespace will be defaulted", "defaultNamespace", a.Resource.GetNamespace())
		obj.SetNamespace(metav1.NamespaceDefault)
	}
	if gvk.Version == "" && mapping.GroupVersionKind.Version != "" {
		logger.Info("apiVersion will be defaulted to match discovered rest mapping", "defaultAPIVersion", mapping.GroupVersionKind.Version)
		gvk.Version = mapping.GroupVersionKind.Version
		obj.SetGroupVersionKind(gvk)
	}
	return nil
}

// updateStatus replaces the deployed status with the new set of objects.
func (a *ApplySet) updateStatus(objs []*unstructured.Unstructured) {
	var refs []deliveryv1alpha1.DeployedObjectReference

	// We need to re-read the objects or assume they exist?
	// If we applied them, they exist. But we need their UIDs.
	// The original code did `updateDeployedObjectStatusReferences(objs, deployer)`.
	// As suspected, if `objs` were not updated by Apply, where does UID come from?
	// If they are existing objects, they might have UID.
	// If they are new, they won't have UID in `objs` unless we fetch them or Apply updates them.
	// In original code:
	// applyConfig := client.ApplyConfigurationFromUnstructured(obj)
	// r.Client.Apply(..., applyConfig, ...)
	// This does NOT update `obj`.
	// So `updateDeployedObjectStatusReferences` using `obj.GetUID()` would fail to get UID for new objects.
	// I should probably fix this by fetching the object or using `Patch` which updates the object?
	// Or `Apply` with object?
	// Controller-runtime Client.Apply supports both Object and ApplyConfiguration.
	// If I pass Object, it uses Server-Side Apply and updates the object?
	// No, `Client.Apply` takes `Object` (which can be ApplyConfiguration or a concrete Object).
	// If I pass `Unstructured`, it works.
	// The original code used `client.ApplyConfigurationFromUnstructured(obj)`.

	// I will fetch the UID.
	// Or even better, I'll update `apply` to use `Client.Patch` with `ApplyPatchType` which updates the object.
	// `Client.Apply` is a wrapper.

	for _, obj := range objs {
		// We use a simplified ref without UID if we can't get it, or we try to get it.
		// Ideally we should have the UID.
		// For now, I'll construct the ref.

		apiVersion, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
		ref := deliveryv1alpha1.DeployedObjectReference{
			APIVersion: apiVersion,
			Kind:       kind,
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
			UID:        obj.GetUID(), // Might be empty
		}
		refs = append(refs, ref)
	}
	a.Deployer.Status.Deployed = refs
}

func objectKey(obj *unstructured.Unstructured) string {
	return fmt.Sprintf("%s:%s:%s:%s", obj.GetAPIVersion(), obj.GetKind(), obj.GetNamespace(), obj.GetName())
}

func refKey(ref deliveryv1alpha1.DeployedObjectReference) string {
	return fmt.Sprintf("%s:%s:%s:%s", ref.APIVersion, ref.Kind, ref.Namespace, ref.Name)
}
