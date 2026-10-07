package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindNamespacedDeployer = "NamespacedDeployer"

// NamespacedDeployerSpec defines the desired state of NamespacedDeployer.
type NamespacedDeployerSpec struct {
	// ResourceRef is the name of an OCM resource in the same namespace containing the ResourceGroupDefinition.
	// +required
	// +kubebuilder:validation:XValidation:rule="self.name != ''",message="resourceRef.name must be set"
	ResourceRef corev1.LocalObjectReference `json:"resourceRef"`

	// ServiceAccountName is the name of the service account in the same namespace that is impersonated
	// to apply and prune the deployed objects. Its RBAC permissions define what the deployer may deploy.
	// If empty, the deployer applies and prunes with the controller's own service account.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// All references must point to the namespace of the NamespacedDeployer.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// NamespacedDeployer.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

func (in *NamespacedDeployer) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *NamespacedDeployer) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func (in *NamespacedDeployer) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.GetNamespace(), in.GetName())
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/resource_version"] = vid

	return metadata
}

func (in *NamespacedDeployer) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *NamespacedDeployer) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *NamespacedDeployer) GetKind() string {
	return KindNamespacedDeployer
}

func (in *NamespacedDeployer) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *NamespacedDeployer) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

func (in *NamespacedDeployer) GetResourceRef() ObjectKey {
	return ObjectKey{Namespace: in.GetNamespace(), Name: in.Spec.ResourceRef.Name}
}

func (in *NamespacedDeployer) GetServiceAccountName() string {
	return in.Spec.ServiceAccountName
}

func (in *NamespacedDeployer) IsSuspended() bool {
	return in.Spec.Suspend
}

func (in *NamespacedDeployer) GetDeployerStatus() *DeployerStatus {
	return &in.Status
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the NamespacedDeployer is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the NamespacedDeployer"

// NamespacedDeployer is the Schema for the namespaceddeployers API.
// Unlike the cluster-scoped Deployer, it applies objects with the permissions of the service account
// referenced in its spec. That service account's RBAC, not this resource, bounds which namespaces and
// kinds it can deploy.
type NamespacedDeployer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespacedDeployerSpec `json:"spec,omitempty"`
	Status DeployerStatus         `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NamespacedDeployerList contains a list of NamespacedDeployer.
type NamespacedDeployerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespacedDeployer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespacedDeployer{}, &NamespacedDeployerList{})
}
