package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ConfigurationPolicy string

const (
	ConfigurationPolicyPropagate      ConfigurationPolicy = "Propagate"
	ConfigurationPolicyDoNotPropagate ConfigurationPolicy = "DoNotPropagate"
)

// OCMConfiguration defines a configuration applied to the reconciliation of an
// ocm k8s object as well as the policy for its propagation of this
// configuration.
// +kubebuilder:validation:XValidation:rule="((!has(self.apiVersion) || self.apiVersion == \"\" || self.apiVersion == \"v1\") && (self.kind == \"Secret\" || self.kind == \"ConfigMap\")) || (self.apiVersion == \"delivery.ocm.software/v1alpha1\" && (self.kind == \"Repository\" || self.kind == \"Component\" || self.kind == \"Resource\" || self.kind == \"Replication\"))",message="apiVersion must be one of \"v1\" with kind \"Secret\" or \"ConfigMap\" or \"delivery.ocm.software/v1alpha1\" with the kind of an OCM kubernetes object"
type OCMConfiguration struct {
	// Ref reference config maps or secrets containing arbitrary
	// ocm config data (in the ocm config file format), or other configurable
	// ocm api objects (Repository, Component, Resource) to
	// reuse their propagated configuration.
	meta.NamespacedObjectKindReference `json:",inline"`
	// Policy affects the propagation behavior of the configuration. If set to
	// ConfigurationPolicyPropagate other ocm api objects can reference this
	// object to reuse this configuration.
	// +kubebuilder:validation:Enum:="Propagate";"DoNotPropagate"
	// +kubebuilder:default:="Propagate"
	// +required
	Policy ConfigurationPolicy `json:"policy,omitempty"`
}

type ObjectKey struct {
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +required
	Name string `json:"name,omitempty"`
}

type Verification struct {
	// +required
	Signature string `json:"signature,omitempty"`
	// Public Key Secret Format
	// A secret containing public keys for signature verification is expected to be of the structure:
	//
	//  Data:
	//	  <Signature-Name>: <PublicKey/Certificate>
	//
	// Additionally, to prepare for a common ocm secret management, it might make sense to introduce a specific secret type
	// for these secrets.
	// +optional
	SecretRef corev1.LocalObjectReference `json:"secretRef,omitempty"`
	// Value defines a PEM/base64 encoded public key value.
	// +optional
	Value string `json:"value,omitempty"`
}

// ResourceID defines the configuration of the repository.
type ResourceID struct {
	// +required
	ByReference ResourceReference `json:"byReference,omitempty"`
	// TODO: Implement BySelector (see https://github.com/open-component-model/ocm-project/issues/296)
}

// ResourceReference defines a reference to a resource akin to the OCM Specification.
// For more details see dedicated guide in the Specification:
// https://github.com/open-component-model/ocm-spec/blob/main/doc/05-guidelines/03-references.md#references
type ResourceReference struct {
	Resource      runtime.Identity   `json:"resource"`
	ReferencePath []runtime.Identity `json:"referencePath,omitempty"`
}

type ComponentInfo struct {
	// +required
	RepositorySpec *apiextensionsv1.JSON `json:"repositorySpec,omitempty"`
	// +required
	Component string `json:"component,omitempty"`
	// +required
	Version string `json:"version,omitempty"`
	// Digest information of the Component, if available as per OCM specification.
	// +optional
	Digest *v2.Digest `json:"digest,omitempty"`
}

type ResourceInfo struct {
	// +required
	Name string `json:"name,omitempty"`
	// +required
	Type string `json:"type,omitempty"`
	// +optional
	Version string `json:"version,omitempty"`
	// +optional
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	Access apiextensionsv1.JSON `json:"access,omitempty"`
	// +optional
	Digest *v2.Digest `json:"digest,omitempty"`
	// +optional
	Labels []Label `json:"labels,omitempty"`
}

type Label struct {
	// Name is the unique name of the label.
	Name string `json:"name"`
	// Value is the json/yaml data of the label
	Value apiextensionsv1.JSON `json:"value"`
	// Version is the optional specification version of the attribute value
	Version string `json:"version,omitempty"`
	// Signing describes whether the label should be included into the signature
	Signing bool `json:"signing,omitempty"`
	// MergeAlgorithm optionally describes the desired merge handling used to
	// merge the label value during a transfer.
	Merge *MergeAlgorithmSpecification `json:"merge,omitempty"`
}

type MergeAlgorithmSpecification struct {
	// Algorithm optionally described the Merge algorithm used to
	// merge the label value during a transfer.
	Algorithm string `json:"algorithm"`
	// Config contains optional config for the merge algorithm.
	Config apiextensionsv1.JSON `json:"config,omitempty"`
}
