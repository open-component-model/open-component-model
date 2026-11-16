package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// Types of the fused transformations that stream a plain blob from its source into a
// component version as a local blob, without buffering it in a file. The source access kind
// (Wget or S3) and the target repository kind (OCI registry or CTF) are part of the type, so
// each spec carries a concretely typed access and repository that are validated against
// their schemas, and globalAccessPolicy only exists where the target is an OCI registry.
const (
	TransferWgetToOCILocalBlobType = "TransferWgetToOCILocalBlob"
	TransferWgetToCTFLocalBlobType = "TransferWgetToCTFLocalBlob"
	TransferS3ToOCILocalBlobType   = "TransferS3ToOCILocalBlob"
	TransferS3ToCTFLocalBlobType   = "TransferS3ToCTFLocalBlob"
)

// TransferToLocalBlobOutput is the output of the Transfer{Wget,S3}To{OCI,CTF}LocalBlob
// transformations.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferToLocalBlobOutput struct {
	// Resource is the resource descriptor as added to the target, with its local blob access.
	Resource *v2.Resource `json:"resource"`
}

// TransferWgetToOCILocalBlob streams the content behind a Wget access into a component
// version in a target OCI repository, where it is stored as a local blob. It replaces the
// DownloadWgetResource + OCIAddLocalResource pair, which buffers the content in a file.
// Only GET requests are accepted: the content may be requested more than once.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferWgetToOCILocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferWgetToOCILocalBlob/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id"`
	Spec   *TransferWgetToOCILocalBlobSpec `json:"spec"`
	Output *TransferToLocalBlobOutput      `json:"output,omitzero"`
}

// TransferWgetToOCILocalBlobSpec is the input specification for the
// TransferWgetToOCILocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferWgetToOCILocalBlobSpec struct {
	// Access is the Wget access the content is read from. Its verb must be GET or empty.
	Access wgetv1.Wget `json:"access"`
	// Repository is the OCI repository specification the local blob is added to.
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access. Its digest,
	// if set, is the digest the content read from Access must have.
	TargetResource *v2.Resource `json:"targetResource"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferWgetToCTFLocalBlob streams the content behind a Wget access into a component
// version in a target CTF repository, where it is stored as a local blob. It replaces the
// DownloadWgetResource + CTFAddLocalResource pair, which buffers the content in a file.
// Only GET requests are accepted: the content may be requested more than once.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferWgetToCTFLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferWgetToCTFLocalBlob/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id"`
	Spec   *TransferWgetToCTFLocalBlobSpec `json:"spec"`
	Output *TransferToLocalBlobOutput      `json:"output,omitzero"`
}

// TransferWgetToCTFLocalBlobSpec is the input specification for the
// TransferWgetToCTFLocalBlob transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferWgetToCTFLocalBlobSpec struct {
	// Access is the Wget access the content is read from. Its verb must be GET or empty.
	Access wgetv1.Wget `json:"access"`
	// Repository is the CTF repository specification the local blob is added to.
	Repository ctf.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access. Its digest,
	// if set, is the digest the content read from Access must have.
	TargetResource *v2.Resource `json:"targetResource"`
}

// TransferS3ToOCILocalBlob streams an S3 object into a component version in a target OCI
// repository, where it is stored as a local blob. It replaces the DownloadS3Resource +
// OCIAddLocalResource pair, which buffers the object in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferS3ToOCILocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferS3ToOCILocalBlob/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id"`
	Spec   *TransferS3ToOCILocalBlobSpec `json:"spec"`
	Output *TransferToLocalBlobOutput    `json:"output,omitzero"`
}

// TransferS3ToOCILocalBlobSpec is the input specification for the TransferS3ToOCILocalBlob
// transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferS3ToOCILocalBlobSpec struct {
	// Access is the S3 access the object is read from.
	Access s3v2.S3 `json:"access"`
	// Repository is the OCI repository specification the local blob is added to.
	Repository oci.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access. Its digest,
	// if set, is the digest the object read from Access must have.
	TargetResource *v2.Resource `json:"targetResource"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferS3ToCTFLocalBlob streams an S3 object into a component version in a target CTF
// repository, where it is stored as a local blob. It replaces the DownloadS3Resource +
// CTFAddLocalResource pair, which buffers the object in a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferS3ToCTFLocalBlob struct {
	// +ocm:jsonschema-gen:enum=TransferS3ToCTFLocalBlob/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id"`
	Spec   *TransferS3ToCTFLocalBlobSpec `json:"spec"`
	Output *TransferToLocalBlobOutput    `json:"output,omitzero"`
}

// TransferS3ToCTFLocalBlobSpec is the input specification for the TransferS3ToCTFLocalBlob
// transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferS3ToCTFLocalBlobSpec struct {
	// Access is the S3 access the object is read from.
	Access s3v2.S3 `json:"access"`
	// Repository is the CTF repository specification the local blob is added to.
	Repository ctf.Repository `json:"repository"`
	// Component is the component name to add the resource to.
	Component string `json:"component"`
	// Version is the component version to add the resource to.
	Version string `json:"version"`
	// TargetResource is the resource descriptor to add, with a local blob access. Its digest,
	// if set, is the digest the object read from Access must have.
	TargetResource *v2.Resource `json:"targetResource"`
}
