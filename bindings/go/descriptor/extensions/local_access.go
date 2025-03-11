package extensions

import (
	"ocm.software/open-component-model/bindings/golang/descriptor"
	"ocm.software/open-component-model/bindings/golang/runtime"
)

// LocalBlobAccessType is the access type of a blob local to a component.
const (
	LocalBlobAccessType   = "localBlob"
	LocalBlobAccessTypeV1 = LocalBlobAccessType + "/" + "v1"
)

// LocalBlob describes the access for a local blob.
type LocalBlob struct {
	runtime.Type `json:"type"`
	// LocalReference is the repository local identity of the blob.
	// it is used by the repository implementation to get access
	// to the blob and if therefore specific to a dedicated repository type.
	LocalReference string `json:"localReference"`
	// MediaType is the media type of the object represented by the blob
	MediaType string `json:"mediaType"`
	// GlobalAccess is an optional field describing a possibility
	// for a global access. If given, it MUST describe a global access method.
	GlobalAccess *descriptor.Access `json:"globalAccess,omitempty"`
	// ReferenceName is an optional static name the object should be
	// use in a local repository context. It is use by a repository
	// to optionally determine a globally referenceable access according
	// to the OCI distribution spec. The result will be stored
	// by the repository in the field ImageReference.
	// The value is typically an OCI repository name optionally
	// followed by a colon ':' and a tag
	ReferenceName string `json:"referenceName,omitempty"`
}

const (
	LocalBlobUploadType = "uploader.localblob"
)

type LocalBlobUpload struct {
	runtime.Type `json:"type"`
}
