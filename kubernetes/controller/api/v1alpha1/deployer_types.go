package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const KindDeployer = "Deployer"

// DeployerSpec defines the desired state of Deployer.
type DeployerSpec struct {
	// ResourceRef is the k8s resource name of an OCM resource containing the ResourceGroupDefinition.
	// +required
	ResourceRef ObjectKey `json:"resourceRef"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Resource.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// DeployerStatus defines the observed state of Deployer.
type DeployerStatus struct {
	// ObservedGeneration is the last observed generation of the Deployer
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Deployer.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Resource reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`

	// Deployed contains references to the objects that have been deployed by the Deployer through
	// the Resource.
	Deployed []DeployedObjectReference `json:"deployed,omitempty"`
}

// DeployedObjectReference is a reference to an object that has been deployed by the Deployer.
// It contains the API version, kind, name, and optionally the namespace of the deployed object.
type DeployedObjectReference struct {
	// API version of the referent.
	APIVersion string `json:"apiVersion"`
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	Namespace string `json:"namespace,omitempty"`
	// UID of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids
	UID types.UID `json:"uid,omitempty"`
}

func (in *Deployer) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *Deployer) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func (in *Deployer) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.GetNamespace(), in.GetName())
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/resource_version"] = vid

	return metadata
}

func (in *Deployer) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *Deployer) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Deployer) GetKind() string {
	return KindDeployer
}

func (in *Deployer) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Deployer) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Deployer is the Schema for the deployers API.
type Deployer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeployerSpec   `json:"spec,omitempty"`
	Status DeployerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeployerList contains a list of Deployer.
type DeployerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Deployer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Deployer{}, &DeployerList{})
}
