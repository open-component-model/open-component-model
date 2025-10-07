package v1alpha1

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindResource = "Resource"

// ResourceSpec defines the desired state of Resource.
type ResourceSpec struct {
	// ComponentRef is a reference to a Component.
	// +required
	ComponentRef corev1.LocalObjectReference `json:"componentRef"`

	// Resource identifies the ocm resource to be fetched.
	// +required
	Resource ResourceID `json:"resource"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// SkipVerify indicates whether the resource should be verified or not.
	// A verification requires the resource to be downloaded, which can be
	// expensive for large resources.
	SkipVerify bool `json:"skipVerify,omitempty"`

	// Interval at which the resource is checked for updates.
	// +required
	Interval metav1.Duration `json:"interval"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Resource.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// AdditionalStatusFields are additional fields that can be used to
	// extend the status of the Resource with custom expressions.
	AdditionalStatusFields map[string]string `json:"additionalStatusFields,omitempty"`
}

// ResourceStatus defines the observed state of Resource.
type ResourceStatus struct {
	// ObservedGeneration is the last observed generation of the Resource
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Resource *ResourceInfo `json:"resource,omitempty"`

	// +optional
	Component *ComponentInfo `json:"component,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Resource reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`

	// +optional
	Additional map[string]apiextensionsv1.JSON `json:"additional,omitempty"`
}

// Resource is the Schema for the resources API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the Resource is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the Resource"
type Resource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceSpec   `json:"spec"`
	Status ResourceStatus `json:"status,omitempty"`
}

func (in *Resource) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *Resource) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func (in *Resource) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.GetNamespace(), in.GetName())
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/resource_version"] = vid

	return metadata
}

func (in *Resource) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *Resource) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Resource) GetKind() string {
	return KindResource
}

// GetRequeueAfter returns the duration after which the Resource must be
// reconciled again.
func (in *Resource) GetRequeueAfter() time.Duration {
	if in == nil {
		return 0
	}
	return in.Spec.Interval.Duration
}

func (in *Resource) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Resource) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

// +kubebuilder:object:root=true

// ResourceList contains a list of Resource.
type ResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Resource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Resource{}, &ResourceList{})
}
