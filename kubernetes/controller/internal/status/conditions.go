package status

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// ConditionObject is satisfied by all CRD types that expose conditions.
type ConditionObject interface {
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

// SetCondition sets the given condition on the object, updating an existing
// condition of the same type or appending a new one.
func SetCondition(obj ConditionObject, condition metav1.Condition) {
	conditions := obj.GetConditions()
	apimeta.SetStatusCondition(&conditions, condition)
	obj.SetConditions(conditions)
}

// RemoveCondition removes a condition of the given type from the object.
func RemoveCondition(obj ConditionObject, conditionType string) {
	conditions := obj.GetConditions()
	apimeta.RemoveStatusCondition(&conditions, conditionType)
	obj.SetConditions(conditions)
}

// FindCondition returns the condition with the given type, or nil.
func FindCondition(obj ConditionObject, conditionType string) *metav1.Condition {
	return apimeta.FindStatusCondition(obj.GetConditions(), conditionType)
}

// IsReady returns true if the ReadyCondition is True.
func IsReady(obj ConditionObject) bool {
	return apimeta.IsStatusConditionTrue(obj.GetConditions(), v1alpha1.ReadyCondition)
}

// IsReconciling returns true if the ReconcilingCondition is True.
func IsReconciling(obj ConditionObject) bool {
	return apimeta.IsStatusConditionTrue(obj.GetConditions(), v1alpha1.ReconcilingCondition)
}

// IsStalled returns true if the StalledCondition is True.
func IsStalled(obj ConditionObject) bool {
	return apimeta.IsStatusConditionTrue(obj.GetConditions(), v1alpha1.StalledCondition)
}
