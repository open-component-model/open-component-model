package v1alpha1

import (
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindRepository = "Repository"

// RepositorySpec defines the desired state of Repository.
type RepositorySpec struct {
	// RepositorySpec is the config of an ocm repository containing component
	// versions. This config has to be a valid ocm repository implementation
	// specification
	// https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/README.md.
	// +required
	RepositorySpec *apiextensionsv1.JSON `json:"repositorySpec"`

	// OCMConfig defines references to secrets, config maps or ocm api
	// objects providing configuration data including credentials.
	// +optional
	OCMConfig []OCMConfiguration `json:"ocmConfig,omitempty"`

	// Interval at which the ocm repository specified by the RepositorySpec
	// validated.
	// +required
	Interval metav1.Duration `json:"interval"`

	// Suspend tells the controller to suspend the reconciliation of this
	// Repository.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// RepositoryStatus defines the observed state of Repository.
type RepositoryStatus struct {
	// ObservedGeneration is the last observed generation of the Repository
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Repository.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// EffectiveOCMConfig specifies the entirety of config maps and secrets
	// whose configuration data was applied to the Repository reconciliation,
	// in the order the configuration data was applied.
	// +optional
	EffectiveOCMConfig []OCMConfiguration `json:"effectiveOCMConfig,omitempty"`
}

// GetConditions returns the conditions of the Repository.
func (in *Repository) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the conditions of the Repository.
func (in *Repository) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// GetRequeueAfter returns the duration after which the ComponentVersion must be
// reconciled again.
func (in *Repository) GetRequeueAfter() time.Duration {
	if in == nil {
		return 0
	}
	return in.Spec.Interval.Duration
}

func (in *Repository) GetSpecifiedOCMConfig() []OCMConfiguration {
	return in.Spec.OCMConfig
}

func (in *Repository) GetEffectiveOCMConfig() []OCMConfiguration {
	return in.Status.EffectiveOCMConfig
}

// GetVID unique identifier of the object.
func (in *Repository) GetVID() map[string]string {
	vid := fmt.Sprintf("%s:%s", in.Namespace, in.Name)
	metadata := make(map[string]string)
	metadata[GroupVersion.Group+"/repository"] = vid

	return metadata
}

func (in *Repository) SetObservedGeneration(v int64) {
	in.Status.ObservedGeneration = v
}

// Repository is the Schema for the repositories API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,description="Indicates if the Resource is Ready",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Displays the Age of the Resource"
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RepositorySpec   `json:"spec"`
	Status RepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RepositoryList contains a list of Repository.
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Repository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Repository{}, &RepositoryList{})
}
