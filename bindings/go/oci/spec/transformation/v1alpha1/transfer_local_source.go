package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The fused local source transfers mirror the local blob (resource) transfers for the
// deprecated component sources whose access is a local blob: one transformation type per
// path, so each spec carries a concretely typed repository, and globalAccessPolicy only
// exists where the target is an OCI registry.
const (
	TransferOCILocalSourceToOCILocalSourceType = "TransferOCILocalSourceToOCILocalSource"
	TransferOCILocalSourceToCTFLocalSourceType = "TransferOCILocalSourceToCTFLocalSource"
	TransferCTFLocalSourceToOCILocalSourceType = "TransferCTFLocalSourceToOCILocalSource"
	TransferCTFLocalSourceToCTFLocalSourceType = "TransferCTFLocalSourceToCTFLocalSource"
)

// OCILocalSource identifies a local source of a component version in an OCI repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCILocalSource struct {
	// Repository is the OCI repository specification holding the component version.
	Repository oci.Repository `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
	// Digest optionally pins the component version to the OCI digest ("<algorithm>:<hex>") of the
	// image index or manifest it is stored as. The source is then read from that artifact
	// instead of from the one the version tag resolves to.
	Digest string `json:"digest,omitempty"`
	// SourceIdentity identifies the source to transfer.
	// Must match a source in the component descriptor.
	SourceIdentity runtime.Identity `json:"sourceIdentity"`
}

// CTFLocalSource identifies a local source of a component version in a CTF repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFLocalSource struct {
	// Repository is the CTF repository specification holding the component version.
	Repository ctf.Repository `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
	// Digest optionally pins the component version to the OCI digest ("<algorithm>:<hex>") of the
	// image index or manifest it is stored as. The source is then read from that artifact
	// instead of from the one the version tag resolves to.
	Digest string `json:"digest,omitempty"`
	// SourceIdentity identifies the source to transfer.
	// Must match a source in the component descriptor.
	SourceIdentity runtime.Identity `json:"sourceIdentity"`
}

// TransferLocalSourceOutput is the output specification of the transformations that stream
// a local source into a component version as a local blob.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferLocalSourceOutput struct {
	// Source is the source descriptor with the local blob access in the target.
	Source *v2.Source `json:"source"`
}

// TransferOCILocalSourceToOCILocalSource is a fused transformation that streams a local
// source of a component version in an OCI repository into the same component version in a
// target OCI repository, where it is stored as a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalSourceToOCILocalSource struct {
	// +ocm:jsonschema-gen:enum=TransferOCILocalSourceToOCILocalSource/v1alpha1
	Type   runtime.Type                                `json:"type"`
	ID     string                                      `json:"id"`
	Spec   *TransferOCILocalSourceToOCILocalSourceSpec `json:"spec"`
	Output *TransferLocalSourceOutput                  `json:"output,omitempty"`
}

// TransferOCILocalSourceToOCILocalSourceSpec is the input specification for the
// TransferOCILocalSourceToOCILocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalSourceToOCILocalSourceSpec struct {
	// Source identifies the local source to transfer.
	Source OCILocalSource `json:"source"`
	// Repository is the OCI repository specification the local source is added to.
	Repository oci.Repository `json:"repository"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferOCILocalSourceToCTFLocalSource is a fused transformation that streams a local
// source of a component version in an OCI repository into the same component version in a
// target CTF repository, where it is stored as a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalSourceToCTFLocalSource struct {
	// +ocm:jsonschema-gen:enum=TransferOCILocalSourceToCTFLocalSource/v1alpha1
	Type   runtime.Type                                `json:"type"`
	ID     string                                      `json:"id"`
	Spec   *TransferOCILocalSourceToCTFLocalSourceSpec `json:"spec"`
	Output *TransferLocalSourceOutput                  `json:"output,omitempty"`
}

// TransferOCILocalSourceToCTFLocalSourceSpec is the input specification for the
// TransferOCILocalSourceToCTFLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCILocalSourceToCTFLocalSourceSpec struct {
	// Source identifies the local source to transfer.
	Source OCILocalSource `json:"source"`
	// Repository is the CTF repository specification the local source is added to.
	Repository ctf.Repository `json:"repository"`
}

// TransferCTFLocalSourceToOCILocalSource is a fused transformation that streams a local
// source of a component version in a CTF repository into the same component version in a
// target OCI repository, where it is stored as a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalSourceToOCILocalSource struct {
	// +ocm:jsonschema-gen:enum=TransferCTFLocalSourceToOCILocalSource/v1alpha1
	Type   runtime.Type                                `json:"type"`
	ID     string                                      `json:"id"`
	Spec   *TransferCTFLocalSourceToOCILocalSourceSpec `json:"spec"`
	Output *TransferLocalSourceOutput                  `json:"output,omitempty"`
}

// TransferCTFLocalSourceToOCILocalSourceSpec is the input specification for the
// TransferCTFLocalSourceToOCILocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalSourceToOCILocalSourceSpec struct {
	// Source identifies the local source to transfer.
	Source CTFLocalSource `json:"source"`
	// Repository is the OCI repository specification the local source is added to.
	Repository oci.Repository `json:"repository"`
	// GlobalAccessPolicy controls whether global access references are added to local blobs.
	// If not set (empty), global access is never added to discourage reliance on global access references.
	// Set to "auto" to auto-detect based on the storage backend.
	//
	// Experimental: This policy is carried over from OCM v1 for backwards compatibility.
	// Its future availability is being evaluated by the community.
	GlobalAccessPolicy oci.GlobalAccessPolicy `json:"globalAccessPolicy,omitempty"`
}

// TransferCTFLocalSourceToCTFLocalSource is a fused transformation that streams a local
// source of a component version in a CTF repository into the same component version in a
// target CTF repository, where it is stored as a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalSourceToCTFLocalSource struct {
	// +ocm:jsonschema-gen:enum=TransferCTFLocalSourceToCTFLocalSource/v1alpha1
	Type   runtime.Type                                `json:"type"`
	ID     string                                      `json:"id"`
	Spec   *TransferCTFLocalSourceToCTFLocalSourceSpec `json:"spec"`
	Output *TransferLocalSourceOutput                  `json:"output,omitempty"`
}

// TransferCTFLocalSourceToCTFLocalSourceSpec is the input specification for the
// TransferCTFLocalSourceToCTFLocalSource transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFLocalSourceToCTFLocalSourceSpec struct {
	// Source identifies the local source to transfer.
	Source CTFLocalSource `json:"source"`
	// Repository is the CTF repository specification the local source is added to.
	Repository ctf.Repository `json:"repository"`
}
