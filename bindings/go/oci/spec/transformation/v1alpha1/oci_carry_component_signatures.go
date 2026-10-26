package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCICarryComponentSignaturesType = "OCICarryComponentSignatures"

// OCICarryComponentSignatures is a transformer specification to copy the
// signature referrers (e.g. cosign signatures and attestations) of a
// normalized-layout component version from a source repository into an OCI
// repository that already holds the same component version.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCICarryComponentSignatures struct {
	// +ocm:jsonschema-gen:enum=OCICarryComponentSignatures/v1alpha1
	Type   runtime.Type                       `json:"type"`
	ID     string                             `json:"id,omitempty"`
	Spec   *OCICarryComponentSignaturesSpec   `json:"spec"`
	Output *OCICarryComponentSignaturesOutput `json:"output,omitempty"`
}

// OCICarryComponentSignaturesOutput is the output specification of the
// OCICarryComponentSignatures.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCICarryComponentSignaturesOutput struct{}

// OCICarryComponentSignaturesSpec is the input specification for the
// OCICarryComponentSignatures.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCICarryComponentSignaturesSpec struct {
	// Repository is the target repository that already holds the component version.
	Repository oci.Repository `json:"repository"`
	// SourceRepository is the repository the component version and its signature
	// referrers are copied from.
	SourceRepository *runtime.Raw `json:"sourceRepository"`
	// Component is the name of the component.
	Component string `json:"component"`
	// Version is the version of the component.
	Version string `json:"version"`
}
