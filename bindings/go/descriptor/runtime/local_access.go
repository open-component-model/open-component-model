package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlobAccessType is the access type of blob local to a component.
const (
	LocalBlobAccessType        = "localBlob"
	LocalBlobAccessTypeVersion = "v1"
)

var typ = runtime.Type{
	Name:    LocalBlobAccessType,
	Version: LocalBlobAccessTypeVersion,
}

func GetLocalBlobAccessType() runtime.Type {
	return typ
}

// LocalBlob describes the access for a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type LocalBlob struct {
	Type runtime.Type `json:"type"`
	// LocalReference is the repository local identity of the blob.
	// it is used by the repository implementation to get access
	// to the blob and if therefore specific to a dedicated repository type.
	LocalReference string `json:"localReference"`
	// MediaType is the media type of the object represented by the blob
	MediaType string `json:"mediaType"`
	// GlobalAccess is an optional field describing a possibility
	// for a global access. If given, it MUST describe a global access method.
	GlobalAccess runtime.Typed `json:"globalAccess,omitempty"`
	// ReferenceName is an optional static name the object should be
	// use in a local repository context. It is use by a repository
	// to optionally determine a globally referenceable access according
	// to the OCI distribution spec. The result will be stored
	// by the repository in the field ImageReference.
	// The value is typically an OCI repository name optionally
	// followed by a colon ':' and a tag
	ReferenceName string `json:"referenceName,omitempty"`
}
