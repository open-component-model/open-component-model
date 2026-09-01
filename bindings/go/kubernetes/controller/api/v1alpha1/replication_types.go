package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Replication.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
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

	// LastFailedTransferEvents contains detailed events about the transfer if it failed.
	// If it's empty, there was no failed last transfer. Is cleaned upon a successful transfer.
	// +optional
	LastFailedTransferEvents []TransferEvent `json:"lastFailedTransferEvents,omitempty"`

	// Component reflects the currently observed source component version.
	// +optional
	Component *ComponentInfo `json:"component,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Replication reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`
}

// TransferEvent captures a single failed transformation observed during a
// replication transfer.
type TransferEvent struct {
	// ID is the identifier of the transformation that failed.
	// +required
	ID string `json:"id"`

	// Name is the display name of the transformation in the form "ID [Type]".
	// +required
	Name string `json:"name"`

	// Error is the error message reported by the failed transformation.
	// +required
	Error string `json:"error"`
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
