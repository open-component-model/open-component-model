package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The fused local blob transfers come in one transformation type per path: the source and
// target repository kinds (OCI registry or CTF) are part of the type, so each spec carries a
// concretely typed repository that is validated against its schema, and globalAccessPolicy
// only exists where the target is an OCI registry.
const (
	TransferOCILocalBlobToOCILocalBlobType = "TransferOCILocalBlobToOCILocalBlob"
	TransferOCILocalBlobToCTFLocalBlobType = "TransferOCILocalBlobToCTFLocalBlob"
	TransferCTFLocalBlobToOCILocalBlobType = "TransferCTFLocalBlobToOCILocalBlob"
	TransferCTFLocalBlobToCTFLocalBlobType = "TransferCTFLocalBlobToCTFLocalBlob"
	TransferOCILocalBlobToOCIArtifactType  = "TransferOCILocalBlobToOCIArtifact"
	TransferCTFLocalBlobToOCIArtifactType  = "TransferCTFLocalBlobToOCIArtifact"
	TransferOCIArtifactToOCILocalBlobType  = "TransferOCIArtifactToOCILocalBlob"
	TransferOCIArtifactToCTFLocalBlobType  = "TransferOCIArtifactToCTFLocalBlob"
)

// OCILocalBlobSource identifies a local resource of a component version in an OCI repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCILocalBlobSource struct {
	// Repository is the OCI repository specification holding the component version.
	Repository oci.Repository `json:"repository"`
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

// CTFLocalBlobSource identifies a local resource of a component version in a CTF repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFLocalBlobSource struct {
	// Repository is the CTF repository specification holding the component version.
	Repository ctf.Repository `json:"repository"`
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

// TransferLocalBlobOutput is the output specification of the transformations that stream
// a local resource into a component version as a local blob.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobOutput struct {
	// Resource is the resource descriptor with the local blob access in the target.
	Resource *v2.Resource `json:"resource"`
}

// TransferOCILocalBlobToOCILocalBlob is a fused transformation that streams a local resource
// of a component version in an OCI repository into the same component version in a target
// OCI repository, where it is stored as a local blob. It replaces the OCIGetLocalResource +
// OCIAddLocalResource pair, which buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToOCILocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferOCILocalBlobToOCILocalBlob/v1alpha1
	Type   runtime.Type                            `json:"type"`
	ID     string                                  `json:"id"`
	Spec   *TransferOCILocalBlobToOCILocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput                `json:"output,omitempty"`
}

// TransferOCILocalBlobToOCILocalBlobSpec is the input specification for the
// TransferOCILocalBlobToOCILocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToOCILocalBlobSpec struct {
	// Source identifies the local resource to transfer.
	Source OCILocalBlobSource `json:"source"`
	// Repository is the OCI repository specification the local blob is added to.
	Repository oci.Repository `json:"repository"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferOCILocalBlobToCTFLocalBlob is a fused transformation that streams a local resource
// of a component version in an OCI repository into the same component version in a target
// CTF repository, where it is stored as a local blob. It replaces the OCIGetLocalResource +
// CTFAddLocalResource pair, which buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToCTFLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferOCILocalBlobToCTFLocalBlob/v1alpha1
	Type   runtime.Type                            `json:"type"`
	ID     string                                  `json:"id"`
	Spec   *TransferOCILocalBlobToCTFLocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput                `json:"output,omitempty"`
}

// TransferOCILocalBlobToCTFLocalBlobSpec is the input specification for the
// TransferOCILocalBlobToCTFLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToCTFLocalBlobSpec struct {
	// Source identifies the local resource to transfer.
	Source OCILocalBlobSource `json:"source"`
	// Repository is the CTF repository specification the local blob is added to.
	Repository ctf.Repository `json:"repository"`
}

// TransferCTFLocalBlobToOCILocalBlob is a fused transformation that streams a local resource
// of a component version in a CTF repository into the same component version in a target
// OCI repository, where it is stored as a local blob. It replaces the CTFGetLocalResource +
// OCIAddLocalResource pair, which buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToOCILocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferCTFLocalBlobToOCILocalBlob/v1alpha1
	Type   runtime.Type                            `json:"type"`
	ID     string                                  `json:"id"`
	Spec   *TransferCTFLocalBlobToOCILocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput                `json:"output,omitempty"`
}

// TransferCTFLocalBlobToOCILocalBlobSpec is the input specification for the
// TransferCTFLocalBlobToOCILocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToOCILocalBlobSpec struct {
	// Source identifies the local resource to transfer.
	Source CTFLocalBlobSource `json:"source"`
	// Repository is the OCI repository specification the local blob is added to.
	Repository oci.Repository `json:"repository"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferCTFLocalBlobToCTFLocalBlob is a fused transformation that streams a local resource
// of a component version in a CTF repository into the same component version in a target
// CTF repository, where it is stored as a local blob. It replaces the CTFGetLocalResource +
// CTFAddLocalResource pair, which buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToCTFLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferCTFLocalBlobToCTFLocalBlob/v1alpha1
	Type   runtime.Type                            `json:"type"`
	ID     string                                  `json:"id"`
	Spec   *TransferCTFLocalBlobToCTFLocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput                `json:"output,omitempty"`
}

// TransferCTFLocalBlobToCTFLocalBlobSpec is the input specification for the
// TransferCTFLocalBlobToCTFLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToCTFLocalBlobSpec struct {
	// Source identifies the local resource to transfer.
	Source CTFLocalBlobSource `json:"source"`
	// Repository is the CTF repository specification the local blob is added to.
	Repository ctf.Repository `json:"repository"`
}

// TransferLocalBlobToOCIArtifactOutput is the output specification of the transformations
// that stream a local resource out of a component version into a separate OCI artifact.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalBlobToOCIArtifactOutput struct {
	// Resource is the updated resource descriptor with the target OCI access.
	Resource *v2.Resource `json:"resource"`
}

// TransferOCILocalBlobToOCIArtifact is a fused transformation that streams a local resource
// holding an OCI artifact out of a component version in an OCI repository and uploads it as a
// separate OCI artifact. It replaces the OCIGetLocalResource + AddOCIArtifact pair, which
// buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=TransferOCILocalBlobToOCIArtifact/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id"`
	Spec   *TransferOCILocalBlobToOCIArtifactSpec `json:"spec"`
	Output *TransferLocalBlobToOCIArtifactOutput  `json:"output,omitempty"`
}

// TransferOCILocalBlobToOCIArtifactSpec is the input specification for the
// TransferOCILocalBlobToOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalBlobToOCIArtifactSpec struct {
	// Source identifies the local resource to transfer.
	Source OCILocalBlobSource `json:"source"`
	// TargetResource is the target resource descriptor with the destination OCI image reference.
	TargetResource *v2.Resource `json:"targetResource"`
}

// TransferCTFLocalBlobToOCIArtifact is a fused transformation that streams a local resource
// holding an OCI artifact out of a component version in a CTF repository and uploads it as a
// separate OCI artifact. It replaces the CTFGetLocalResource + AddOCIArtifact pair, which
// buffers the blob in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=TransferCTFLocalBlobToOCIArtifact/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id"`
	Spec   *TransferCTFLocalBlobToOCIArtifactSpec `json:"spec"`
	Output *TransferLocalBlobToOCIArtifactOutput  `json:"output,omitempty"`
}

// TransferCTFLocalBlobToOCIArtifactSpec is the input specification for the
// TransferCTFLocalBlobToOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalBlobToOCIArtifactSpec struct {
	// Source identifies the local resource to transfer.
	Source CTFLocalBlobSource `json:"source"`
	// TargetResource is the target resource descriptor with the destination OCI image reference.
	TargetResource *v2.Resource `json:"targetResource"`
}

// TransferOCIArtifactToOCILocalBlob is a fused transformation that streams an OCI artifact
// (or a single layer) from its registry into a component version in a target OCI repository,
// where it is stored as a local blob. It replaces the GetOCIArtifact + OCIAddLocalResource
// pair, which buffers the artifact in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToOCILocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferOCIArtifactToOCILocalBlob/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id"`
	Spec   *TransferOCIArtifactToOCILocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput               `json:"output,omitempty"`
}

// TransferOCIArtifactToOCILocalBlobSpec is the input specification for the
// TransferOCIArtifactToOCILocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToOCILocalBlobSpec struct {
	// Resource is the source resource descriptor with an OCI image or OCI image layer access.
	Resource *v2.Resource `json:"resource"`
	// Repository is the OCI repository specification the local blob is added to.
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access.
	TargetResource *v2.Resource `json:"targetResource"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferOCIArtifactToCTFLocalBlob is a fused transformation that streams an OCI artifact
// (or a single layer) from its registry into a component version in a target CTF repository,
// where it is stored as a local blob. It replaces the GetOCIArtifact + CTFAddLocalResource
// pair, which buffers the artifact in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToCTFLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferOCIArtifactToCTFLocalBlob/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id"`
	Spec   *TransferOCIArtifactToCTFLocalBlobSpec `json:"spec"`
	Output *TransferLocalBlobOutput               `json:"output,omitempty"`
}

// TransferOCIArtifactToCTFLocalBlobSpec is the input specification for the
// TransferOCIArtifactToCTFLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIArtifactToCTFLocalBlobSpec struct {
	// Resource is the source resource descriptor with an OCI image or OCI image layer access.
	Resource *v2.Resource `json:"resource"`
	// Repository is the CTF repository specification the local blob is added to.
	Repository ctf.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access.
	TargetResource *v2.Resource `json:"targetResource"`
}
