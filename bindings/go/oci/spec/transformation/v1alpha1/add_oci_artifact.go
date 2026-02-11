package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const AddOCIArtifactType = "AddOCIArtifact"

// AddOCIArtifact is a transformer specification to add an OCI artifact
// to a OCI registry
// TODO(matthiasbruns): add more docs
// Spec: AddOCIArtifactSpec
// Output: AddOCIArtifactOutput
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=AddOCIArtifact/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id"`
	Spec   *AddOCIArtifactSpec   `json:"spec"`
	Output *AddOCIArtifactOutput `json:"output,omitempty"`
}

// AddOCIArtifactOutput is the output specification of the
// AddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifactOutput struct {
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// AddOCIArtifactSpec is the input specification for the
// AddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifactSpec struct {
	// OCIArtifact is the OCI artifact to be added, represented as a blob.
	OCIArtifact blob.ReadOnlyBlob `json:"ociArtifact"`
	// TargetRegistry is the target OCI repository
	TargetRegistry string `json:"targetRef"`
}
