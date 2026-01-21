package applyset

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (a *applySet) loadParentState(ctx context.Context) error {
	// Get the current state of the parent from the cluster
	parentObj := &unstructured.Unstructured{}
	parentObj.SetGroupVersionKind(a.parent.GetObjectKind().GroupVersionKind())

	if err := a.k8sClient.Get(ctx, client.ObjectKeyFromObject(a.parent), parentObj); err != nil {
		if apierrors.IsNotFound(err) {
			// Parent doesn't exist yet, that's okay
			a.currentLabels = make(map[string]string)
			a.currentAnnotations = make(map[string]string)
			return nil
		}
		return fmt.Errorf("failed to get parent object: %w", err)
	}

	a.currentLabels = parentObj.GetLabels()
	if a.currentLabels == nil {
		a.currentLabels = make(map[string]string)
	}
	a.currentAnnotations = parentObj.GetAnnotations()
	if a.currentAnnotations == nil {
		a.currentAnnotations = make(map[string]string)
	}

	return nil
}

func (a *applySet) recordNamespace(obj *unstructured.Unstructured, mapping *meta.RESTMapping) error {
	gvk := obj.GetObjectKind().GroupVersionKind()

	switch mapping.Scope.Name() {
	case meta.RESTScopeNameNamespace:
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = a.parent.GetNamespace()
			if namespace == "" {
				namespace = metav1.NamespaceDefault
			}
		}
		a.desiredNamespaces.Insert(namespace)
	case meta.RESTScopeNameRoot:
		if obj.GetNamespace() != "" {
			return fmt.Errorf("namespace was provided for cluster-scoped object %v %v", gvk, obj.GetName())
		}
	default:
		return fmt.Errorf("unknown scope for gvk %s: %q", gvk, mapping.Scope.Name())
	}

	return nil
}
