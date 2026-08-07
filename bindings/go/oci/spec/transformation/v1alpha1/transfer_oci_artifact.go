package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const TransferOCIArtifactType = "TransferOCIArtifact"

// TransferOCIArtifact is a fused transformation that streams an OCI artifact
// directly from a source registry to a target registry without creating an
// intermediate tar file. It replaces the separate GetOCIArtifact + AddOCIArtifact
// pair when both endpoints support streaming.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=TransferOCIArtifact/v1alpha1
	Type   runtime.Type               `json:"type"`
	ID     string                     `json:"id"`
	Spec   *TransferOCIArtifactSpec   `json:"spec"`
	Output *TransferOCIArtifactOutput `json:"output,omitempty"`
}

// TransferOCIArtifactSpec is the input specification for the
// TransferOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactSpec struct {
	// Resource is the source resource descriptor with OCI image access.
	Resource *v2.Resource `json:"resource"`
	// TargetResource is the target resource descriptor with the destination OCI image reference.
	TargetResource *v2.Resource `json:"targetResource"`
}

// TransferOCIArtifactOutput is the output specification for the
// TransferOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactOutput struct {
	// Resource is the updated resource descriptor with target access and pinned digest.
	Resource *v2.Resource `json:"resource"`
}
