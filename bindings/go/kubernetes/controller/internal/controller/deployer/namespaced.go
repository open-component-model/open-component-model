package deployer

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
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

// orphanOnDeletion reports what a deletion leaves behind when the service account cannot prune. GC removes children
// in the deployer's namespace through their owner reference, the rest stays and is listed in a Warning event.
func (r *Reconciler) orphanOnDeletion(ctx context.Context, deployer deliveryv1alpha1.DeployerObject, metadata applyset.Metadata, pruneErr error) {
	logger := log.FromContext(ctx)

	orphaned, err := r.orphanedObjects(ctx, metadata)
	if err != nil {
		// Without read access the controller can only name the scope the objects live in.
		logger.Error(err, "failed to list orphaned objects")
		orphaned = []string{metadata.PruneScope().String()}
	}
	logger.Info("skipping ApplySet prune as the service account cannot prune", "reason", pruneErr.Error(), "orphaned", len(orphaned))
	if len(orphaned) == 0 {
		return
	}

	status.MarkNotReady(r.EventRecorder, deployer, deliveryv1alpha1.DeletionFailedReason, fmt.Sprintf(
		"service account %s cannot prune (%v), orphaning objects outside of namespace %s: %s",
		deployer.GetServiceAccountName(), pruneErr, deployer.GetNamespace(), strings.Join(orphaned, ", ")))
}

// orphanedObjects lists the ApplySet members that are cluster-scoped or in the ApplySet's additional namespaces. The
// controller reads them with its own identity, which it needs anyway to watch them for drift.
func (r *Reconciler) orphanedObjects(ctx context.Context, metadata applyset.Metadata) ([]string, error) {
	selector := client.MatchingLabels{applyset.ApplysetPartOfLabel: metadata.ID}

	var orphaned []string
	for gk := range metadata.GroupKinds {
		mapping, err := r.resourceRESTMapper.RESTMapping(gk)
		if err != nil {
			return nil, fmt.Errorf("failed to map %s: %w", gk, err)
		}

		namespaces := []string{""}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			namespaces = sets.List(metadata.AdditionalNamespaces)
		}
		for _, namespace := range namespaces {
			list := &metav1.PartialObjectMetadataList{}
			list.SetGroupVersionKind(mapping.GroupVersionKind.GroupVersion().WithKind(mapping.GroupVersionKind.Kind + "List"))
			if err := r.apiReader.List(ctx, list, selector, client.InNamespace(namespace)); err != nil {
				return nil, fmt.Errorf("failed to list %s: %w", gk, err)
			}
			for _, item := range list.Items {
				name := item.GetName()
				if item.GetNamespace() != "" {
					name = item.GetNamespace() + "/" + name
				}
				orphaned = append(orphaned, gk.String()+" "+name)
			}
		}
	}
	slices.Sort(orphaned)

	return orphaned, nil
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
