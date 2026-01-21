package applyset

import (
	"context"
	"fmt"
	"strings"

	slogcontext "github.com/veqryn/slog-context"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (a *applySet) injectToolLabels(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	if a.toolLabels != nil {
		for k, v := range a.toolLabels {
			labels[k] = v
		}
	}
	return labels
}

func (a *applySet) injectApplySetLabels(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ApplySetPartOfLabel] = a.ID()
	return labels
}

func (a *applySet) updateParentLabelsAndAnnotations(ctx context.Context, useSuperset bool) error {
	logger := slogcontext.FromCtx(ctx)

	// Generate desired labels and annotations
	desiredLabels := a.desiredParentLabels()
	desiredAnnotations, namespaces, gks := a.desiredParentAnnotations(useSuperset)

	// Track superset for later use
	if useSuperset {
		a.supersetNamespaces = namespaces
		a.supersetGKs = gks
	}

	// Check if we need to update
	if equality.Semantic.DeepEqual(a.currentLabels, desiredLabels) &&
		equality.Semantic.DeepEqual(a.currentAnnotations, desiredAnnotations) {
		logger.Info("parent labels and annotations unchanged, skipping update")
		return nil
	}

	// Create a patch for the parent using controller-runtime client
	parentPatch := &unstructured.Unstructured{}
	parentPatch.SetGroupVersionKind(a.parent.GetObjectKind().GroupVersionKind())
	parentPatch.SetName(a.parent.GetName())
	parentPatch.SetNamespace(a.parent.GetNamespace())
	parentPatch.SetLabels(desiredLabels)
	parentPatch.SetAnnotations(desiredAnnotations)

	// Apply using controller-runtime client
	err := a.k8sClient.Patch(ctx, parentPatch, client.Apply,
		client.FieldOwner(a.fieldManager+"-parent"))
	if err != nil {
		return fmt.Errorf("error updating parent: %w", err)
	}

	logger.Info("updated parent labels and annotations")

	// Update current state
	a.currentLabels = desiredLabels
	a.currentAnnotations = desiredAnnotations

	return nil
}

func (a *applySet) desiredParentLabels() map[string]string {
	labels := make(map[string]string)
	labels[ApplySetParentIDLabel] = a.ID()
	toolingID := a.toolingID.String()
	// deployer.delivery.ocm.software/v1alpha1 would fail due to '/' in the value
	// convert to deployer.delivery.ocm.software.v1alpha1
	toolingID = strings.ReplaceAll(toolingID, "/", ".")
	labels[ApplySetToolingLabel] = toolingID
	return labels
}
