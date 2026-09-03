package externalartifact

import (
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// Type aliases to the Flux upstream API so the rest of the package does not
// depend on the exact import paths.
type (
	fluxArtifact                      = fluxmeta.Artifact
	fluxNamespacedObjectKindReference = fluxmeta.NamespacedObjectKindReference
)

// Artifact Ready-condition reasons, matching the Flux source-controller /
// RFC-0012 values.
const (
	reasonSucceeded   = fluxmeta.SucceededReason
	reasonFetchFailed = "FetchFailed"
)

// setReadyCondition sets the Ready condition on an ExternalArtifact.
func setReadyCondition(ea *sourcev1.ExternalArtifact, status metav1.ConditionStatus, reason, message string) {
	conditions := ea.Status.Conditions
	apimeta.SetStatusCondition(&conditions, metav1.Condition{
		Type:               fluxmeta.ReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ea.GetGeneration(),
	})
	ea.Status.Conditions = conditions
}

// revisionIdentifier returns the human-readable identifier prefixed to the
// artifact revision (before the "@<algorithm>:<checksum>" suffix). It prefers
// the OCM resource version, falling back to the component version.
func revisionIdentifier(resource *v1alpha1.Resource, matched *descriptor.Resource) string {
	if matched != nil && matched.Version != "" {
		return matched.Version
	}
	if resource.Status.Resource != nil && resource.Status.Resource.Version != "" {
		return resource.Status.Resource.Version
	}
	if resource.Status.Component != nil && resource.Status.Component.Version != "" {
		return resource.Status.Component.Version
	}

	return "latest"
}

// ResourceReadyChangedPredicate triggers reconciliation when a Resource's Ready
// condition or its resolved resource/component status changes, so the artifact
// is refreshed when the underlying OCM content is updated.
type ResourceReadyChangedPredicate struct {
	predicate.Funcs
}

// Update implements the predicate for update events.
func (ResourceReadyChangedPredicate) Update(e event.UpdateEvent) bool {
	oldResource, ok := e.ObjectOld.(*v1alpha1.Resource)
	if !ok {
		return false
	}
	newResource, ok := e.ObjectNew.(*v1alpha1.Resource)
	if !ok {
		return false
	}

	oldReady := apimeta.IsStatusConditionTrue(oldResource.Status.Conditions, v1alpha1.ReadyCondition)
	newReady := apimeta.IsStatusConditionTrue(newResource.Status.Conditions, v1alpha1.ReadyCondition)
	if oldReady != newReady {
		return true
	}

	// Trigger when the resolved resource digest changes (new content).
	return resourceDigest(oldResource) != resourceDigest(newResource)
}

// resourceDigest returns the resolved OCM resource digest from the Resource
// status, or "" when absent.
func resourceDigest(resource *v1alpha1.Resource) string {
	if resource.Status.Resource == nil || resource.Status.Resource.Digest == nil {
		return ""
	}

	return resource.Status.Resource.Digest.Value
}
