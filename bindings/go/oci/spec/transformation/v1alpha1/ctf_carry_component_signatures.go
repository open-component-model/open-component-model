package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFCarryComponentSignaturesType = "CTFCarryComponentSignatures"

// CTFCarryComponentSignatures is a transformer specification to copy the
// signature referrers (e.g. cosign signatures and attestations) of a
// normalized-layout component version from a source repository into a CTF
// repository that already holds the same component version.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFCarryComponentSignatures struct {
	// +ocm:jsonschema-gen:enum=CTFCarryComponentSignatures/v1alpha1
	Type   runtime.Type                       `json:"type"`
	ID     string                             `json:"id,omitempty"`
	Spec   *CTFCarryComponentSignaturesSpec   `json:"spec"`
	Output *CTFCarryComponentSignaturesOutput `json:"output,omitempty"`
}

// CTFCarryComponentSignaturesOutput is the output specification of the
// CTFCarryComponentSignatures.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFCarryComponentSignaturesOutput struct{}

// CTFCarryComponentSignaturesSpec is the input specification for the
// CTFCarryComponentSignatures.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFCarryComponentSignaturesSpec struct {
	// Repository is the target repository that already holds the component version.
	Repository ctf.Repository `json:"repository"`
	// SourceRepository is the repository the component version and its signature
	// referrers are copied from.
	SourceRepository *runtime.Raw `json:"sourceRepository"`
	// Component is the name of the component.
	Component string `json:"component"`
	// Version is the version of the component.
	Version string `json:"version"`
}
