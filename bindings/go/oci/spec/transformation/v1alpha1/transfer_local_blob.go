package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	TransferLocalBlobType              = "TransferLocalBlob"
	TransferLocalBlobToOCIArtifactType = "TransferLocalBlobToOCIArtifact"
	TransferOCIArtifactToLocalBlobType = "TransferOCIArtifactToLocalBlob"
)

// LocalBlobSource identifies a local resource of a component version in an OCI or CTF
// repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type LocalBlobSource struct {
	// Repository is the OCI or CTF repository specification holding the component version.
	Repository *runtime.Raw `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
	// Digest optionally pins the component version to the OCI digest ("<algorithm>:<hex>") of the
	// image index or manifest it is stored as. The resource is then read from that artifact
	// instead of from the one the version tag resolves to.
	Digest string `json:"digest,omitempty"`
	// ResourceIdentity identifies the resource to transfer.
	// Must match a resource in the component descriptor.
	ResourceIdentity runtime.Identity `json:"resourceIdentity"`
}

// TransferLocalBlob is a fused transformation that streams a local resource of a
// component version into the same component version in a target OCI or CTF repository,
// where it is stored as a local blob. It replaces the GetLocalResource + AddLocalResource
// pair, which buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferLocalBlob/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *TransferLocalBlobSpec   `json:"spec"`
	Output *TransferLocalBlobOutput `json:"output,omitempty"`
}

// TransferLocalBlobSpec is the input specification for the
// TransferLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobSpec struct {
	// Source identifies the local resource to transfer.
	Source LocalBlobSource `json:"source"`
	// Repository is the OCI or CTF repository specification the local blob is added to.
	Repository *runtime.Raw `json:"repository"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs
	// in an OCI repository. If not set (empty), global access is never added.
	// Set to "auto" to auto-detect based on the storage backend. Only OCI repositories support "auto".
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferLocalBlobOutput is the output specification for the
// TransferLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobOutput struct {
	// Resource is the resource descriptor with the local blob access in the target.
	Resource *v2.Resource `json:"resource"`
}

// TransferLocalBlobToOCIArtifact is a fused transformation that streams a local resource
// holding an OCI artifact out of a component version and uploads it as a separate OCI
// artifact. It replaces the GetLocalResource + AddOCIArtifact pair, which buffers the
// blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobToOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=TransferLocalBlobToOCIArtifact/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferLocalBlobToOCIArtifactSpec   `json:"spec"`
	Output *TransferLocalBlobToOCIArtifactOutput `json:"output,omitempty"`
}

// TransferLocalBlobToOCIArtifactSpec is the input specification for the
// TransferLocalBlobToOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobToOCIArtifactSpec struct {
	// Source identifies the local resource to transfer.
	Source LocalBlobSource `json:"source"`
	// TargetResource is the target resource descriptor with the destination OCI image reference.
	TargetResource *v2.Resource `json:"targetResource"`
}

// TransferLocalBlobToOCIArtifactOutput is the output specification for the
// TransferLocalBlobToOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobToOCIArtifactOutput struct {
	// Resource is the updated resource descriptor with the target OCI access.
	Resource *v2.Resource `json:"resource"`
}

// TransferOCIArtifactToLocalBlob is a fused transformation that streams an OCI artifact
// (or a single layer) from its registry into a component version in a target OCI or CTF
// repository, where it is stored as a local blob. It replaces the GetOCIArtifact +
// AddLocalResource pair, which buffers the artifact in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferOCIArtifactToLocalBlob/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferOCIArtifactToLocalBlobSpec   `json:"spec"`
	Output *TransferOCIArtifactToLocalBlobOutput `json:"output,omitempty"`
}

// TransferOCIArtifactToLocalBlobSpec is the input specification for the
// TransferOCIArtifactToLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToLocalBlobSpec struct {
	// Resource is the source resource descriptor with an OCI image or OCI image layer access.
	Resource *v2.Resource `json:"resource"`
	// Repository is the OCI or CTF repository specification the local blob is added to.
	Repository *runtime.Raw `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access.
	TargetResource *v2.Resource `json:"targetResource"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs
	// in an OCI repository. If not set (empty), global access is never added.
	// Set to "auto" to auto-detect based on the storage backend. Only OCI repositories support "auto".
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferOCIArtifactToLocalBlobOutput is the output specification for the
// TransferOCIArtifactToLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToLocalBlobOutput struct {
	// Resource is the resource descriptor with the local blob access in the target.
	Resource *v2.Resource `json:"resource"`
}
