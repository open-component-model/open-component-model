package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The fused component version transfers come in one transformation type per path, like the fused
// local blob transfers: the source and target repository kinds (OCI registry or CTF) are part of
// the type, so each spec carries concretely typed repositories that are validated against their
// schemas.
const (
	TransferOCIComponentVersionToOCIType = "TransferOCIComponentVersionToOCI"
	TransferOCIComponentVersionToCTFType = "TransferOCIComponentVersionToCTF"
	TransferCTFComponentVersionToOCIType = "TransferCTFComponentVersionToOCI"
	TransferCTFComponentVersionToCTFType = "TransferCTFComponentVersionToCTF"
)

// OCIComponentVersionSource identifies a component version stored in the normalized layout in an
// OCI repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIComponentVersionSource struct {
	// Repository is the OCI repository specification holding the component version.
	Repository oci.Repository `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
	// Digest optionally pins the component version to the OCI digest ("<algorithm>:<hex>") of the
	// normalized manifest it is stored as. The manifest and its signature referrers are then read
	// from that artifact instead of from the one the version tag resolves to.
	Digest string `json:"digest,omitempty"`
}

// CTFComponentVersionSource identifies a component version stored in the normalized layout in a
// CTF repository.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFComponentVersionSource struct {
	// Repository is the CTF repository specification holding the component version.
	Repository ctf.Repository `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
	// Digest optionally pins the component version to the OCI digest ("<algorithm>:<hex>") of the
	// normalized manifest it is stored as. The manifest and its signature referrers are then read
	// from that artifact instead of from the one the version tag resolves to.
	Digest string `json:"digest,omitempty"`
}

// TransferComponentVersionOutput is the output specification of the transformations that
// transfer a component version stored in the normalized layout.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferComponentVersionOutput struct{}

// TransferOCIComponentVersionToOCI is a fused transformation that adds a component version
// stored in the normalized layout in an OCI repository to a target OCI repository. It copies the
// normalized manifest with its signature referrers (e.g. cosign signatures and attestations)
// from the source and writes the descriptor as the access referrer of the target. It replaces
// OCIAddComponentVersion, which rebuilds the manifest and leaves those referrers behind.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIComponentVersionToOCI struct {
	// +ocm:jsonschema-gen:enum=TransferOCIComponentVersionToOCI/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferOCIComponentVersionToOCISpec `json:"spec"`
	Output *TransferComponentVersionOutput       `json:"output,omitzero"`
}

// TransferOCIComponentVersionToOCISpec is the input specification for the
// TransferOCIComponentVersionToOCI transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIComponentVersionToOCISpec struct {
	// Source identifies the component version to transfer.
	Source OCIComponentVersionSource `json:"source"`
	// Repository is the OCI repository specification the component version is added to.
	Repository oci.Repository `json:"repository"`
	// Descriptor is the component descriptor to add. It must normalize to the normalized
	// descriptor of the source: only its accesses may differ.
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// TransferOCIComponentVersionToCTF is a fused transformation that adds a component version
// stored in the normalized layout in an OCI repository to a target CTF repository. It copies the
// normalized manifest with its signature referrers (e.g. cosign signatures and attestations)
// from the source and writes the descriptor as the access referrer of the target. It replaces
// CTFAddComponentVersion, which rebuilds the manifest and leaves those referrers behind.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferOCIComponentVersionToCTF struct {
	// +ocm:jsonschema-gen:enum=TransferOCIComponentVersionToCTF/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferOCIComponentVersionToCTFSpec `json:"spec"`
	Output *TransferComponentVersionOutput       `json:"output,omitzero"`
}

// TransferOCIComponentVersionToCTFSpec is the input specification for the
// TransferOCIComponentVersionToCTF transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferOCIComponentVersionToCTFSpec struct {
	// Source identifies the component version to transfer.
	Source OCIComponentVersionSource `json:"source"`
	// Repository is the CTF repository specification the component version is added to.
	Repository ctf.Repository `json:"repository"`
	// Descriptor is the component descriptor to add. It must normalize to the normalized
	// descriptor of the source: only its accesses may differ.
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// TransferCTFComponentVersionToOCI is a fused transformation that adds a component version
// stored in the normalized layout in a CTF repository to a target OCI repository. It copies the
// normalized manifest with its signature referrers (e.g. cosign signatures and attestations)
// from the source and writes the descriptor as the access referrer of the target. It replaces
// OCIAddComponentVersion, which rebuilds the manifest and leaves those referrers behind.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFComponentVersionToOCI struct {
	// +ocm:jsonschema-gen:enum=TransferCTFComponentVersionToOCI/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferCTFComponentVersionToOCISpec `json:"spec"`
	Output *TransferComponentVersionOutput       `json:"output,omitzero"`
}

// TransferCTFComponentVersionToOCISpec is the input specification for the
// TransferCTFComponentVersionToOCI transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFComponentVersionToOCISpec struct {
	// Source identifies the component version to transfer.
	Source CTFComponentVersionSource `json:"source"`
	// Repository is the OCI repository specification the component version is added to.
	Repository oci.Repository `json:"repository"`
	// Descriptor is the component descriptor to add. It must normalize to the normalized
	// descriptor of the source: only its accesses may differ.
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// TransferCTFComponentVersionToCTF is a fused transformation that adds a component version
// stored in the normalized layout in a CTF repository to a target CTF repository. It copies the
// normalized manifest with its signature referrers (e.g. cosign signatures and attestations)
// from the source and writes the descriptor as the access referrer of the target. It replaces
// CTFAddComponentVersion, which rebuilds the manifest and leaves those referrers behind.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TransferCTFComponentVersionToCTF struct {
	// +ocm:jsonschema-gen:enum=TransferCTFComponentVersionToCTF/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id"`
	Spec   *TransferCTFComponentVersionToCTFSpec `json:"spec"`
	Output *TransferComponentVersionOutput       `json:"output,omitzero"`
}

// TransferCTFComponentVersionToCTFSpec is the input specification for the
// TransferCTFComponentVersionToCTF transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type TransferCTFComponentVersionToCTFSpec struct {
	// Source identifies the component version to transfer.
	Source CTFComponentVersionSource `json:"source"`
	// Repository is the CTF repository specification the component version is added to.
	Repository ctf.Repository `json:"repository"`
	// Descriptor is the component descriptor to add. It must normalize to the normalized
	// descriptor of the source: only its accesses may differ.
	Descriptor *v2.Descriptor `json:"descriptor"`
}
