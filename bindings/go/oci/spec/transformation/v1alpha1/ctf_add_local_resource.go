package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFAddLocalResourceType = "CTFAddLocalResource"

// CTFAddLocalResource is a transformer specification to add a local resource
// blob to a component version in a CTF repository.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalResource struct {
	// +ocm:jsonschema-gen:enum=CTFAddLocalResource/v1alpha1
	Type   runtime.Type               `json:"type"`
	ID     string                     `json:"id"`
	Spec   *CTFAddLocalResourceSpec   `json:"spec"`
	Output *CTFAddLocalResourceOutput `json:"output,omitempty"`
}

// CTFAddLocalResourceOutput is the output specification of the
// CTFAddLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalResourceOutput struct {
	// Resource is the updated resource descriptor with populated LocalReference
	Resource *v2.Resource `json:"resource"`
}

// CTFAddLocalResourceSpec is the input specification for the
// CTFAddLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddLocalResourceSpec struct {
	// Repository is the CTF repository specification
	Repository ctf.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// Resource is the resource descriptor to add.
	// If the Resource contains an access specification, it may be used
	// by the underlying implementation to derive metadata to avoid additional compute
	// (such as digest information) or to steer implementation (such as a reference name)
	Resource *v2.Resource `json:"resource"`
	// File is the access specification to the data that should be added
	File v1alpha1.File `json:"file"`
}
