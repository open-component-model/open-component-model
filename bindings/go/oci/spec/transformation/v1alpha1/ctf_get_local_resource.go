package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFGetLocalResourceType = "CTFGetLocalResource"

// CTFGetLocalResource is a transformer specification to get a local resource
// blob from a component version in a CTF repository and buffer it to a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFGetLocalResource struct {
	// +ocm:jsonschema-gen:enum=CTFGetLocalResource/v1alpha1
	Type   runtime.Type               `json:"type"`
	ID     string                     `json:"id,omitempty"`
	Spec   *CTFGetLocalResourceSpec   `json:"spec"`
	Output *CTFGetLocalResourceOutput `json:"output,omitempty"`
}

// CTFGetLocalResourceOutput is the output specification of the
// CTFGetLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetLocalResourceOutput struct {
	// File is the file access specification for the downloaded resource
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the repository
	Resource *v2.Resource `json:"resource"`
}

// CTFGetLocalResourceSpec is the input specification for the
// CTFGetLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetLocalResourceSpec struct {
	// Repository is the CTF repository specification
	Repository ctf.Repository `json:"repository"`
	// Component is the component name
	Component string `json:"component"`
	// Version is the component version
	Version string `json:"version"`
	// ResourceIdentity identifies the resource to retrieve.
	// Must match a resource in the component descriptor.
	ResourceIdentity runtime.Identity `json:"resourceIdentity"`
	// OutputPath is the path where the blob should be buffered.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
