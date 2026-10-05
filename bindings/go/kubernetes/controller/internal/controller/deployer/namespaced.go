package deployer

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deliveryv1alpha1 "ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

const (
	// impersonatedClientCacheSize bounds the number of cached clients, one per impersonated service account.
	impersonatedClientCacheSize = 256

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

func (r *Reconciler) metricsLabel() string {
	if r.Namespaced {
		return "namespaced" + deployerManager + "/resources"
	}

	return deployerManager + "/resources"
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

// applyClient returns the client used to apply and prune the deployed objects.
func (r *Reconciler) applyClient(ctx context.Context, deployer deliveryv1alpha1.DeployerObject) (client.Client, error) {
	if !r.Namespaced {
		return r.Client, nil
	}

	name := deployer.GetServiceAccountName()
	if name == "" {
		return nil, fmt.Errorf("service account name must be set on %s %s/%s",
			deliveryv1alpha1.KindNamespacedDeployer, deployer.GetNamespace(), deployer.GetName())
	}

	key := client.ObjectKey{Namespace: deployer.GetNamespace(), Name: name}
	if err := r.apiReader.Get(ctx, key, &corev1.ServiceAccount{}); err != nil {
		return nil, fmt.Errorf("failed to get service account %s: %w", key, err)
	}

	username := fmt.Sprintf("system:serviceaccount:%s:%s", key.Namespace, key.Name)
	if c, ok := r.impersonatedClients.Get(username); ok {
		return c, nil
	}

	cfg := rest.CopyConfig(r.restConfig)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: username}
	c, err := client.New(cfg, client.Options{Scheme: r.Scheme, Mapper: r.resourceRESTMapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create client impersonating %s: %w", username, err)
	}
	r.impersonatedClients.Add(username, c)

	return c, nil
}

// orphanOnDeletion keeps deletion from blocking when the service account or its RBAC is gone. GC removes children in
// the deployer's namespace through their owner reference; all other children are orphaned and reported.
func (r *Reconciler) orphanOnDeletion(deployer deliveryv1alpha1.DeployerObject, err error) bool {
	if !r.Namespaced || (!apierrors.IsNotFound(err) && !apierrors.IsForbidden(err)) {
		return false
	}

	var orphaned []string
	for _, ref := range deployer.GetDeployerStatus().Deployed {
		if ref.Namespace != deployer.GetNamespace() {
			orphaned = append(orphaned, fmt.Sprintf("%s %s", ref.Kind, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}))
		}
	}
	if len(orphaned) > 0 {
		status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, fmt.Sprintf(
			"service account %s cannot prune (%v), orphaning objects outside of namespace %s: %s",
			deployer.GetServiceAccountName(), err, deployer.GetNamespace(), strings.Join(orphaned, ", ")))
	}

	return true
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
