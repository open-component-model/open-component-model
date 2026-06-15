package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
)

const KindReplication = "Replication"

// ReplicationSpec defines the desired state of Replication.
type ReplicationSpec struct {
	// ComponentRef is a reference to a Component whose resolved version is transferred.
	// +required
	ComponentRef corev1.LocalObjectReference `json:"componentRef"`

	// TargetRepositoryRef is a reference to the Repository the transfer happens to.
	// +required
	TargetRepositoryRef corev1.LocalObjectReference `json:"targetRepositoryRef"`

	// TransferConfig defines how the transfer is performed. Each entry is either a
	// reference to a ConfigMap holding a generic ocm config, or an inline generic
	// ocm config value. Exactly one of the two must be set per entry.
	// +optional
	TransferConfig []TransferConfig `json:"transferConfig,omitempty"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Replication.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// TransferConfig selects the transfer configuration for a Replication. Provide it
// either as a reference to a ConfigMap holding a generic ocm config with a
// transfer.config.ocm.software entry, or inline via Value carrying the generic
// ocm config object directly on the CR. Exactly one of the two must be set.
type TransferConfig struct {
	// NamespaceName references the ConfigMap holding the generic config by name and
	// optional namespace, defaulting to the Replication namespace.
	// +optional
	*NamespaceName `json:",inline"`

	// Value carries the generic ocm config object inline on the Replication.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// +optional
	Value *generic.Config `json:"value,omitempty"`
}

// ReplicationStatus defines the observed state of Replication.
type ReplicationStatus struct {
	// ObservedGeneration is the last observed generation of the Replication
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Replication.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastTransferredVersion records the component version of the last successful transfer.
	// +optional
	LastTransferredVersion string `json:"lastTransferredVersion,omitempty"`

	// LastTransferredDigest records the component digest of the last successful transfer.
	// +optional
	LastTransferredDigest string `json:"lastTransferredDigest,omitempty"`

	// Component reflects the currently observed source component version.
	// +optional
	Component *ComponentInfo `json:"component,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Replication reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`
}

// Replication is the Schema for the replications API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the Replication is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the Replication"
type Replication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicationSpec   `json:"spec"`
	Status ReplicationStatus `json:"status,omitempty"`
}

func (in *Replication) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *Replication) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

func (in *Replication) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.GetNamespace(), in.GetName())
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/replication_version"] = vid

	return metadata
}

func (in *Replication) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

func (in *Replication) GetObjectMeta() *metav1.ObjectMeta {
	return &in.ObjectMeta
}

func (in *Replication) GetKind() string {
	return KindReplication
}

func (in *Replication) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Replication) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

// +kubebuilder:object:root=true

// ReplicationList contains a list of Replication.
type ReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Replication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Replication{}, &ReplicationList{})
}
