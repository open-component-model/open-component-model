package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIAddLocalResourceType = "OCIAddLocalResource"

// OCIAddLocalResource is a transformer specification to add a local resource
// blob to a component version in an OCI repository.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalResource struct {
	// +ocm:jsonschema-gen:enum=OCIAddLocalResource/v1alpha1
	Type   runtime.Type               `json:"type"`
	ID     string                     `json:"id,omitempty"`
	Spec   *OCIAddLocalResourceSpec   `json:"spec"`
	Output *OCIAddLocalResourceOutput `json:"output,omitempty"`
}

// OCIAddLocalResourceOutput is the output specification of the
// OCIAddLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalResourceOutput struct {
	// Resource is the updated resource descriptor with populated LocalReference
	Resource *v2.Resource `json:"resource"`
}

// OCIAddLocalResourceSpec is the input specification for the
// OCIAddLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddLocalResourceSpec struct {
	// Repository is the OCI repository specification
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// Resource is the resource descriptor to add.
	// If the Resource contains an access specification, it may be used
	// by the underlying implementation to derive metadata to avoid additional compute
	// (such as digest information) or to steer implementation (such as a reference name)
	Resource *v2.Resource `json:"resource"`
	// File is the access specification to the file that should be added
	File v1alpha1.File `json:"file"`
}
