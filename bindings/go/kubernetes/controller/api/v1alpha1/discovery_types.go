package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindDiscovery = "Discovery"

// Selector filters elements of the transitive component graph of a Discovery.
// All specified clauses are ANDed. A nil or empty selector matches everything.
type Selector struct {
	// MatchIdentity matches elements whose identity contains all specified
	// key-value pairs. Keys must be present even when compared against an
	// empty value.
	// +optional
	MatchIdentity map[string]string `json:"matchIdentity,omitempty"`

	// MatchLabels matches elements carrying labels with the specified string
	// values. Non-string label values are matched via Expression only.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// Expression is a CEL expression evaluated for each element. It must
	// evaluate to a boolean. An empty expression is a no-op.
	// +optional
	Expression string `json:"expression,omitempty"`
}

// Extract projects the filtered Discovery result into free-form records.
// Exactly one extraction mode must be specified.
// +kubebuilder:validation:XValidation:rule="(has(self.byResources) ? 1 : 0) + (has(self.byComponents) ? 1 : 0) + (has(self.expression) ? 1 : 0) == 1",message="exactly one of byResources, byComponents, or expression must be specified"
type Extract struct {
	// ByResources evaluates each map value as a CEL expression once per
	// surviving (component, resource) pair with the bindings component and
	// resource. An explicitly empty map emits one empty record per pair.
	// +optional
	ByResources map[string]string `json:"byResources,omitzero"`

	// ByComponents evaluates each map value as a CEL expression once per
	// surviving component with the binding component. An explicitly empty map
	// emits one empty record per component.
	// +optional
	ByComponents map[string]string `json:"byComponents,omitzero"`

	// Expression is a single CEL expression evaluated once over the complete
	// filtered descriptor list with the binding components. It must evaluate
	// to a list of objects.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Expression string `json:"expression,omitempty"`
}

// DiscoverySpec defines the desired state of Discovery.
type DiscoverySpec struct {
	// ComponentRef is a reference to a Component in the same namespace whose
	// transitive component graph is discovered.
	// +required
	// +kubebuilder:validation:XValidation:rule="self.name.size() > 0",message="name must not be empty"
	ComponentRef corev1.LocalObjectReference `json:"componentRef"`

	// ReferenceSelector filters the references of all resolved descriptors.
	// Only reference targets with at least one matching incoming reference are
	// kept. If unset, all references (and the root component) are kept.
	// +optional
	ReferenceSelector *Selector `json:"referenceSelector,omitempty"`

	// ComponentSelector filters the components of the filtered graph.
	// If unset, all components are kept.
	// +optional
	ComponentSelector *Selector `json:"componentSelector,omitempty"`

	// ResourceSelector filters the resources of each surviving component.
	// Components with zero surviving resources are kept. If unset, all
	// resources are kept.
	// +optional
	ResourceSelector *Selector `json:"resourceSelector,omitempty"`

	// Extract projects the filtered components into free-form records
	// published in status.extracted. If unset, the filtered raw v2 descriptors
	// are published in status.components instead.
	// +optional
	Extract *Extract `json:"extract,omitempty"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Discovery.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// DiscoveryStatus defines the observed state of Discovery.
// +kubebuilder:validation:XValidation:rule="!(has(self.components) && has(self.extracted))",message="components and extracted cannot be set at the same time"
type DiscoveryStatus struct {
	// ObservedGeneration is the last observed generation of the Discovery
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Discovery.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Components contains the filtered raw v2 descriptors of the discovered
	// graph, sorted lexicographically by (component.name, component.version).
	// It is only set when spec.extract is unset. A selected but empty result
	// is an empty list; an uncomputed result is absent.
	// +optional
	// +kubebuilder:validation:items:Type=object
	// +kubebuilder:validation:items:XPreserveUnknownFields
	Components []apiextensionsv1.JSON `json:"components,omitzero"`

	// Extracted contains the records projected from the filtered components
	// by spec.extract. It is only set when spec.extract is set. A selected but
	// empty result is an empty list; an uncomputed result is absent.
	// +optional
	// +kubebuilder:validation:items:Type=object
	// +kubebuilder:validation:items:XPreserveUnknownFields
	Extracted []apiextensionsv1.JSON `json:"extracted,omitzero"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Discovery reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`
}

// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries,verbs=get;list;watch
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=discoveries/status,verbs=get;update;patch

// Discovery is the Schema for the discoveries API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the Discovery is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the Resource"
type Discovery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DiscoverySpec   `json:"spec"`
	Status DiscoveryStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the Discovery.
func (in *Discovery) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the conditions of the Discovery.
func (in *Discovery) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// GetVID unique identifier of the object.
func (in *Discovery) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.GetNamespace(), in.GetName())
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/discovery_version"] = vid

	return metadata
}

func (in *Discovery) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *Discovery) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Discovery) GetKind() string {
	return KindDiscovery
}

func (in *Discovery) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Discovery) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

// +kubebuilder:object:root=true

// DiscoveryList contains a list of Discovery.
type DiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Discovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Discovery{}, &DiscoveryList{})
}
