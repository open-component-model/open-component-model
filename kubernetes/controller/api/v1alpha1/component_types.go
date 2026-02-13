package v1alpha1

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DowngradePolicy string

var (
	DowngradePolicyAllow   DowngradePolicy = "Allow"
	DowngradePolicyDeny    DowngradePolicy = "Deny"
	DowngradePolicyEnforce DowngradePolicy = "Enforce"
)

const KindComponent = "Component"

// ComponentSpec defines the desired state of Component.
type ComponentSpec struct {
	// RepositoryRef is a reference to a Repository.
	// +required
	RepositoryRef corev1.LocalObjectReference `json:"repositoryRef"`

	// Component is the name of the ocm component.
	// +required
	Component string `json:"component"`

	// DowngradePolicy specifies whether the component may be
	// downgraded. The property is an enum with the 3 states: `Enforce`, `Allow`,
	// `Deny`, with `Deny` being the default.
	// `Deny` means never allow downgrades (thus, never fetch components with a
	// version lower than the version currently deployed).
	// `Allow` means that the component will be checked for a label with the
	// `ocm.software/ocm-k8s-toolkit/downgradePolicy` which may specify a semver
	// constraint down to which version downgrades are allowed.
	// `Enforce` means always allow downgrades.
	// +kubebuilder:validation:Enum:=Allow;Deny;Enforce
	// +kubebuilder:default:=Deny
	// +optional
	DowngradePolicy DowngradePolicy `json:"downgradePolicy,omitempty"`

	// Semver defines the constraint of the fetched version. '>=v0.1'.
	// +required
	Semver string `json:"semver"`

	// SemverFilter is a regex pattern to filter the versions within the Semver
	// range.
	// +optional
	SemverFilter string `json:"semverFilter,omitempty"`

	// Verify contains a signature name specifying the component signature to be
	// verified as well as the trusted public keys (or certificates containing
	// the public keys) used to verify the signature.
	// TODO: remove in favor of config https://github.com/open-component-model/ocm-project/issues/866
	// +optional
	Verify []Verification `json:"verify,omitempty"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Interval at which the repository will be checked for new component
	// versions.
	// +required
	Interval metav1.Duration `json:"interval"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Component.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// ComponentStatus defines the observed state of Component.
type ComponentStatus struct {
	// ObservedGeneration is the last observed generation of the ComponentStatus
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Component.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Component specifies the concrete version of the component that was
	// fetched after based on the semver constraints during the last successful
	// reconciliation.
	// +optional
	Component ComponentInfo `json:"component,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Component reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`
}

// Component is the Schema for the components API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the Resource is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the Resource"
type Component struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComponentSpec   `json:"spec"`
	Status ComponentStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the Component.
func (in *Component) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the conditions of the Component.
func (in *Component) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// GetVID unique identifier of the object.
func (in *Component) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.Status.Component.Component, in.Status.Component.Version)
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/component_version"] = vid

	return metadata
}

func (in *Component) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *Component) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Component) GetKind() string {
	return "Component"
}

// GetRequeueAfter returns the duration after which the ComponentVersion must be
// reconciled again.
func (in *Component) GetRequeueAfter() time.Duration {
	if in == nil {
		return 0
	}
	return in.Spec.Interval.Duration
}

func (in *Component) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Component) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

func (in *Component) GetVerifications() []Verification {
	return in.Spec.Verify
}

// +kubebuilder:object:root=true

// ComponentList contains a list of Component.
type ComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Component `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Component{}, &ComponentList{})
}
