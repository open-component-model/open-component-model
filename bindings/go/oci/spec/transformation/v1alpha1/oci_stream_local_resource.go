package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIStreamLocalResourceType = "OCIStreamLocalResource"

// OCIStreamLocalResource is a fused transformer specification that streams a
// by-value source resource (for example a wget or s3 access) straight into a
// component version in an OCI repository as a local blob, using the OCI chunked
// streaming push, without buffering the content to a temporary file.
//
// It replaces the separate Download* + OCIAddLocalResource pair for a streaming
// source so the stream never has to cross a transformation-graph node boundary.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIStreamLocalResource struct {
	// +ocm:jsonschema-gen:enum=OCIStreamLocalResource/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id"`
	Spec   *OCIStreamLocalResourceSpec   `json:"spec"`
	Output *OCIStreamLocalResourceOutput `json:"output,omitempty"`
}

// OCIStreamLocalResourceOutput is the output specification of the
// OCIStreamLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIStreamLocalResourceOutput struct {
	// Resource is the updated resource descriptor with a LocalBlob access.
	Resource *v2.Resource `json:"resource"`
}

// OCIStreamLocalResourceSpec is the input specification for the
// OCIStreamLocalResource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIStreamLocalResourceSpec struct {
	// Repository is the OCI repository specification of the target.
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// Resource is the SOURCE resource descriptor. Its access (for example a wget
	// or s3 access) and its digest are used to open a lazy source stream that is
	// pushed into the target as a local blob.
	Resource *v2.Resource `json:"resource"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}
