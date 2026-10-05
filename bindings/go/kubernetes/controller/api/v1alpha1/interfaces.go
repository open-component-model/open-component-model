package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConditionAccessor provides access to an object's conditions.
// +kubebuilder:object:generate=false
type ConditionAccessor interface {
	GetConditions() []metav1.Condition
	SetConditions(conditions []metav1.Condition)
}

// ConfigRefProvider are objects that provide configurations such as credentials
// or other ocm configuration. The interface allows all implementers to use the
// same function to retrieve its configuration.
// +kubebuilder:object:generate=false
type ConfigRefProvider interface {
	client.Object

	// GetSpecifiedOCMConfig returns the configurations specifically specified
	// in the spec of the ocm k8s object.
	// CAREFUL: The configurations retrieved from this method might reference
	// other configurable OCM objects (Repository, Component, Resource,
	// Replication). In that case the EffectiveOCMConfig (referencing Secrets or
	// ConfigMaps) propagated by the referenced ocm k8s objects have to be
	// resolved (see ocm.GetEffectiveConfig).
	GetSpecifiedOCMConfig() []OCMConfiguration

	// GetEffectiveOCMConfig returns the effective configurations propagated by
	// the ocm k8s object.
	GetEffectiveOCMConfig() []OCMConfiguration
}

// OCMK8SObject is a composite interface that the ocm-k8s-toolkit resources implement which allows them to use
// the same ocm context configuration function.
// +kubebuilder:object:generate=false
type OCMK8SObject interface {
	client.Object
	ConditionAccessor
	ConfigRefProvider
}

// DeployerObject is implemented by Deployer and NamespacedDeployer so both kinds share one reconciler.
// +kubebuilder:object:generate=false
type DeployerObject interface {
	OCMK8SObject
	// ObjectKind provides the GroupVersionKind used for the ApplySet ID.
	schema.ObjectKind

	GetVID() map[string]string
	SetObservedGeneration(v int64)

	// GetResourceRef returns the referenced Resource. An empty namespace defaults to the deployer's namespace.
	GetResourceRef() ObjectKey
	// GetServiceAccountName returns the service account to impersonate, or empty for the controller identity.
	GetServiceAccountName() string
	IsSuspended() bool
	GetDeployerStatus() *DeployerStatus
}
