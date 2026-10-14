package deployer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deliveryv1alpha1 "ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/applyset"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

const (
	// defaultServiceAccount exists in every namespace and has no permissions unless an operator grants them.
	defaultServiceAccount = "default"

	// namespacedDeployerAnnotation maps a deployed object back to its NamespacedDeployer ("namespace/name"), as
	// cluster-scoped and cross-namespace objects cannot carry an owner reference to a namespaced owner.
	namespacedDeployerAnnotation = "delivery.ocm.software/namespaced-deployer"
)

func (r *Reconciler) newObject() deliveryv1alpha1.DeployerObject {
	if r.Namespaced {
		return &deliveryv1alpha1.NamespacedDeployer{}
	}

	return &deliveryv1alpha1.Deployer{}
}

func (r *Reconciler) newList() client.ObjectList {
	if r.Namespaced {
		return &deliveryv1alpha1.NamespacedDeployerList{}
	}

	return &deliveryv1alpha1.DeployerList{}
}

func (r *Reconciler) managedBy() string {
	if r.Namespaced {
		return namespacedDeployerManager
	}

	return deployerManager
}

func (r *Reconciler) metricsLabel() string {
	return r.managedBy() + "/resources"
}

func (r *Reconciler) defaultNamespace(deployer deliveryv1alpha1.DeployerObject) string {
	if r.Namespaced {
		return deployer.GetNamespace()
	}

	return metav1.NamespaceDefault
}

func (r *Reconciler) ownerReferenceOptions() []controllerutil.OwnerReferenceOption {
	if r.Namespaced {
		// blockOwnerDeletion would require the impersonated service account to update the deployer's finalizers
		// when the OwnerReferencesPermissionEnforcement admission plugin is enabled.
		return []controllerutil.OwnerReferenceOption{controllerutil.WithBlockOwnerDeletion(false)}
	}

	return nil
}

// serviceAccountName returns the service account a NamespacedDeployer impersonates. Without one in its spec it falls
// back to the namespace's default service account, never to the controller's identity.
func serviceAccountName(deployer deliveryv1alpha1.DeployerObject) string {
	if name := deployer.GetServiceAccountName(); name != "" {
		return name
	}

	return defaultServiceAccount
}

// applyClient returns the client used to apply and prune the deployed objects. A Deployer applies with the
// controller's own identity, a NamespacedDeployer always impersonates a service account.
func (r *Reconciler) applyClient(ctx context.Context, deployer deliveryv1alpha1.DeployerObject) (client.Client, error) {
	if !r.Namespaced {
		return r.Client, nil
	}

	key := client.ObjectKey{Namespace: deployer.GetNamespace(), Name: serviceAccountName(deployer)}
	if err := r.apiReader.Get(ctx, key, &corev1.ServiceAccount{}); err != nil {
		return nil, fmt.Errorf("failed to get service account %s: %w", key, err)
	}

	username := fmt.Sprintf("system:serviceaccount:%s:%s", key.Namespace, key.Name)
	cfg := rest.CopyConfig(r.restConfig)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: username}
	c, err := client.New(cfg, client.Options{Scheme: r.Scheme, Mapper: r.resourceRESTMapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create client impersonating %s: %w", username, err)
	}

	return c, nil
}

// blocksDeletion reports whether a forbidden request keeps the deployer from being deleted. Missing permissions can
// be restored, so a NamespacedDeployer stays and retries as long as its deletion would orphan objects.
func (r *Reconciler) blocksDeletion(ctx context.Context, deployer deliveryv1alpha1.DeployerObject, metadata applyset.Metadata, pruneErr error) bool {
	if !r.Namespaced || !apierrors.IsForbidden(pruneErr) {
		return true
	}

	serviceAccount := fmt.Sprintf("the controller cannot read service account %s", serviceAccountName(deployer))

	if deployer.GetDeletionPolicy() == deliveryv1alpha1.DeletionPolicyOrphan {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, fmt.Sprintf(
			"%s (%v), restore the permissions to release the objects for the Orphan deletion policy", serviceAccount, pruneErr))

		return true
	}

	orphaned, err := r.orphanedObjects(ctx, metadata)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list objects the deletion would orphan")
		orphaned = []string{metadata.PruneScope().String()}
	}
	if len(orphaned) == 0 {
		return false
	}

	status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, fmt.Sprintf(
		"%s (%v), restore the permissions to delete the objects outside of namespace %s: %s",
		serviceAccount, pruneErr, deployer.GetNamespace(), strings.Join(orphaned, ", ")))
	log.FromContext(ctx).Error(errors.New("cannot read service account"), "failed to list objects the deletion would orphan", "serviceAccount", serviceAccount)

	return true
}

// orphanOnDeletion reports what a deletion leaves behind when the service account is gone. GC removes children
// in the deployer's namespace through their owner reference, the rest stays and is listed in a Warning event.
func (r *Reconciler) orphanOnDeletion(ctx context.Context, deployer deliveryv1alpha1.DeployerObject, metadata applyset.Metadata, pruneErr error) {
	logger := log.FromContext(ctx)

	orphaned, err := r.orphanedObjects(ctx, metadata)
	if err != nil {
		// Without read access the controller can only name the scope the objects live in.
		logger.Error(err, "failed to list orphaned objects")
		orphaned = []string{metadata.PruneScope().String()}
	}
	logger.Info("skipping ApplySet prune as the service account is gone", "reason", pruneErr.Error(), "orphaned", len(orphaned))
	if len(orphaned) == 0 {
		return
	}

	status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, fmt.Sprintf(
		"service account %s is gone (%v), orphaning objects outside of namespace %s: %s",
		serviceAccountName(deployer), pruneErr, deployer.GetNamespace(), strings.Join(orphaned, ", ")))
}

// orphanedObjects lists the ApplySet members that are cluster-scoped or in the ApplySet's additional namespaces. The
// controller reads them with its own identity, which it needs anyway to watch them for drift.
func (r *Reconciler) orphanedObjects(ctx context.Context, metadata applyset.Metadata) ([]string, error) {
	members, err := r.applySetMembers(ctx, metadata, sets.List(metadata.AdditionalNamespaces))
	if err != nil {
		return nil, err
	}

	orphaned := make([]string, 0, len(members))
	for _, item := range members {
		name := item.GetName()
		if item.GetNamespace() != "" {
			name = item.GetNamespace() + "/" + name
		}
		orphaned = append(orphaned, item.GroupVersionKind().GroupKind().String()+" "+name)
	}
	slices.Sort(orphaned)

	return orphaned, nil
}

// applySetMembers lists the cluster-scoped ApplySet members and the namespaced ones in the given namespaces.
func (r *Reconciler) applySetMembers(ctx context.Context, metadata applyset.Metadata, namespaces []string) ([]metav1.PartialObjectMetadata, error) {
	selector := client.MatchingLabels{applyset.ApplysetPartOfLabel: metadata.ID}

	var members []metav1.PartialObjectMetadata
	for gk := range metadata.GroupKinds {
		mapping, err := r.resourceRESTMapper.RESTMapping(gk)
		if err != nil {
			return nil, fmt.Errorf("failed to map %s: %w", gk, err)
		}

		scopes := []string{""}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			scopes = namespaces
		}
		for _, namespace := range scopes {
			list := &metav1.PartialObjectMetadataList{}
			list.SetGroupVersionKind(mapping.GroupVersionKind.GroupVersion().WithKind(mapping.GroupVersionKind.Kind + "List"))
			if err := r.apiReader.List(ctx, list, selector, client.InNamespace(namespace)); err != nil {
				return nil, fmt.Errorf("failed to list %s: %w", gk, err)
			}
			for _, item := range list.Items {
				item.SetGroupVersionKind(mapping.GroupVersionKind)
				members = append(members, item)
			}
		}
	}

	return members, nil
}

// propagationPolicy returns the propagation policy of every prune. Orphan only applies to deleting the deployer,
// objects that a newer component version no longer contains are still deleted.
func propagationPolicy(deployer deliveryv1alpha1.DeployerObject) *metav1.DeletionPropagation {
	var policy metav1.DeletionPropagation
	switch deployer.GetDeletionPolicy() {
	case "":
		return nil
	case deliveryv1alpha1.DeletionPolicyForeground:
		policy = metav1.DeletePropagationForeground
	default:
		policy = metav1.DeletePropagationBackground
	}

	return &policy
}

// releaseOnDeletion removes the ApplySet label, the owner reference and the deployer annotation from every deployed
// object. Neither the garbage collector nor a later prune removes them then, and another ApplySet can adopt them.
func (r *Reconciler) releaseOnDeletion(ctx context.Context, c client.Client, deployer deliveryv1alpha1.DeployerObject, metadata applyset.Metadata) error {
	namespaces := append(sets.List(metadata.AdditionalNamespaces), deployer.GetNamespace())
	members, err := r.applySetMembers(ctx, metadata, namespaces)
	if err != nil {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason,
			fmt.Sprintf("failed to list the objects to release for the Orphan deletion policy: %v", err))

		return err
	}

	var errs []error
	for i := range members {
		obj := &members[i]
		patch := client.MergeFromWithOptions(obj.DeepCopy(), client.MergeFromWithOptimisticLock{})

		labels := obj.GetLabels()
		delete(labels, applyset.ApplysetPartOfLabel)
		if labels[managedByLabel] == namespacedDeployerManager {
			delete(labels, managedByLabel)
		}
		obj.SetLabels(labels)
		annotations := obj.GetAnnotations()
		delete(annotations, namespacedDeployerAnnotation)
		obj.SetAnnotations(annotations)
		obj.SetOwnerReferences(slices.DeleteFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
			return ref.UID == deployer.GetUID()
		}))

		if err := c.Patch(ctx, obj, patch); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to release %s %s/%s: %w", obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		msg := fmt.Sprintf("failed to release the objects for the Orphan deletion policy, retrying: %v", err)
		if apierrors.IsForbidden(err) {
			msg = fmt.Sprintf("service account %s cannot release the objects for the Orphan deletion policy, "+
				"restore its permissions to finish the deletion: %v", serviceAccountName(deployer), err)
		}
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, msg)

		return err
	}

	log.FromContext(ctx).Info("released ApplySet members for the Orphan deletion policy", "released", len(members))

	return nil
}

// canOwn reports whether the deployer can be the controller owner of obj. Kubernetes rejects namespaced owners for
// cluster-scoped and cross-namespace dependents.
func (r *Reconciler) canOwn(deployer deliveryv1alpha1.DeployerObject, obj client.Object, namespaced bool) bool {
	return !r.Namespaced || (namespaced && obj.GetNamespace() == deployer.GetNamespace())
}

func setNamespacedDeployerAnnotation(obj client.Object, deployer deliveryv1alpha1.DeployerObject) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[namespacedDeployerAnnotation] = client.ObjectKeyFromObject(deployer).String()
	obj.SetAnnotations(annotations)
}

// childEventHandler enqueues the deployer of a changed deployed object. A Deployer always owns its objects, a
// NamespacedDeployer is found through its annotation.
func (r *Reconciler) childEventHandler(mgr ctrl.Manager) handler.EventHandler {
	if !r.Namespaced {
		return handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), r.newObject(), handler.OnlyControllerOwner())
	}

	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		namespace, name, ok := strings.Cut(obj.GetAnnotations()[namespacedDeployerAnnotation], "/")
		if !ok {
			return nil
		}

		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: namespace, Name: name}}}
	})
}

// validateConfigNamespaces rejects configuration references outside the namespace of a NamespacedDeployer, so
// tenants cannot read configuration and credentials of other namespaces through the controller.
func (r *Reconciler) validateConfigNamespaces(deployer deliveryv1alpha1.DeployerObject, configs []deliveryv1alpha1.OCMConfiguration) error {
	if !r.Namespaced {
		return nil
	}

	for _, cfg := range configs {
		if cfg.Namespace != "" && cfg.Namespace != deployer.GetNamespace() {
			return fmt.Errorf("ocm configuration %s %s/%s is outside of namespace %s",
				cfg.Kind, cfg.Namespace, cfg.Name, deployer.GetNamespace())
		}
	}

	return nil
}
