package v1alpha1

import (
	"github.com/fluxcd/pkg/runtime/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	conditions.Setter
	ConfigRefProvider
}

// VerificationProvider are objects that may provide verification information. The interface allows all implementers to
// use the same function to retrieve and parse the contained or referenced public keys.
// +kubebuilder:object:generate=false
type VerificationProvider interface {
	GetNamespace() string
	GetVerifications() []Verification
}
