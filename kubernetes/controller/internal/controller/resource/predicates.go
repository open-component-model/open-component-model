package resource

import (
	"reflect"

	"github.com/fluxcd/pkg/runtime/conditions"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// ComponentInfoChangedPredicate filters Component Update events to only
// those where Status.Component (ComponentInfo) or the readiness condition
// changed. This prevents condition-only patches that don't affect readiness
// (e.g. MarkReady when already ready) from triggering spurious Resource
// reconciles.
// Create, Delete, and Generic events always pass through.
type ComponentInfoChangedPredicate struct {
	predicate.Funcs
}

func (ComponentInfoChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldComponent, ok := e.ObjectOld.(*v1alpha1.Component)
	if !ok {
		return false
	}

	newComponent, ok := e.ObjectNew.(*v1alpha1.Component)
	if !ok {
		return false
	}

	if !reflect.DeepEqual(oldComponent.Status.Component, newComponent.Status.Component) {
		return true
	}

	if conditions.IsReady(oldComponent) != conditions.IsReady(newComponent) {
		return true
	}

	return false
}
