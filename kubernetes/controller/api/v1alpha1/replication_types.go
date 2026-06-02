package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindReplication = "Replication"

// CopyMode controls which resources are included in a transfer.
type CopyMode string

const (
	// CopyModeLocalBlob inlines resource blobs into the component descriptor at the target. Default.
	CopyModeLocalBlob CopyMode = "localBlob"
	// CopyModeAllResources transfers every resource as a standalone artifact, keeping external references intact.
	CopyModeAllResources CopyMode = "allResources"
)

// ReplicationSpec defines the desired state of Replication.
type ReplicationSpec struct {
	// ComponentRef is a reference to a Component whose resolved version is transferred.
	// +required
	ComponentRef corev1.LocalObjectReference `json:"componentRef"`

	// TargetRepositoryRef is a reference to the Repository the transfer happens to.
	// +required
	TargetRepositoryRef corev1.LocalObjectReference `json:"targetRepositoryRef"`

	// TransferConfig defines how the transfer is performed.
	// +optional
	TransferConfig TransferConfig `json:"transferConfig,omitempty"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Replication.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// TransferConfig defines the transfer configuration for a Replication.
//
// NOTE: only the inlined variant is supported for now until we decide
// what to do about the runtime config.
type TransferConfig struct {
	// Inlined defines the transfer configuration inline.
	// +optional
	Inlined *InlineTransferConfig `json:"inlined,omitempty"`
}

// InlineTransferConfig defines an inlined transfer configuration.
type InlineTransferConfig struct {
	// Recursive controls whether referenced component versions are transferred alongside the root.
	// +optional
	Recursive bool `json:"recursive,omitempty"`

	// CopyMode controls which resources are included in the transfer.
	// +kubebuilder:validation:Enum:="localBlob";"allResources"
	// +kubebuilder:default:="localBlob"
	// +optional
	CopyMode CopyMode `json:"copyMode,omitempty"`
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
