package applyset

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (a *applySet) listObjectsForGK(ctx context.Context, mapping *meta.RESTMapping, namespaces sets.Set[string]) ([]*unstructured.Unstructured, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			ApplySetPartOfLabel: a.ID(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create label selector: %w", err)
	}

	listOptions := &client.ListOptions{
		LabelSelector: labelSelector,
	}

	var allObjects []*unstructured.Unstructured

	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// List in each namespace
		for ns := range namespaces {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(mapping.GroupVersionKind)

			listOptions.Namespace = ns
			err := a.k8sClient.List(ctx, list, listOptions)
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue // Namespace might not exist
				}
				return nil, fmt.Errorf("failed to list %s in namespace %s: %w", mapping.GroupVersionKind, ns, err)
			}
			for i := range list.Items {
				allObjects = append(allObjects, &list.Items[i])
			}
		}
	} else {
		// Cluster-scoped resource
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(mapping.GroupVersionKind)

		err := a.k8sClient.List(ctx, list, listOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to list cluster-scoped %s: %w", mapping.GroupVersionKind, err)
		}
		for i := range list.Items {
			allObjects = append(allObjects, &list.Items[i])
		}
	}

	return allObjects, nil
}

func (a *applySet) getGKsFromAnnotations() []string {
	gksAnnotation := a.currentAnnotations[ApplySetGKsAnnotation]
	if gksAnnotation == "" {
		return nil
	}

	gks := strings.Split(gksAnnotation, ",")
	result := make([]string, 0, len(gks))
	for _, gk := range gks {
		gk = strings.TrimSpace(gk)
		if gk != "" {
			result = append(result, gk)
		}
	}
	return result
}

func (a *applySet) getNamespacesToCheck() sets.Set[string] {
	namespaces := sets.New[string]()

	// Add parent's namespace if namespaced
	if a.parent.GetNamespace() != "" {
		namespaces.Insert(a.parent.GetNamespace())
	}

	// Add additional namespaces from annotation
	nsAnnotation := a.currentAnnotations[ApplySetAdditionalNamespacesAnnotation]
	if nsAnnotation != "" {
		for _, ns := range strings.Split(nsAnnotation, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				namespaces.Insert(ns)
			}
		}
	}

	return namespaces
}
