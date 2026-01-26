package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	FileType       = "File"
	LegacyFileType = "file"
)

// File describes the access to a file.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type File struct {
	// +ocm:jsonschema-gen:enum=File/v1alpha1,file/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=File,file
	Type runtime.Type `json:"type"`
	// URI must conform to RFC8089 and is a locator to the file.
	// Note that implementations may still choose to reject URIs that if they do not fully
	// implement RFC8089.
	URI string `json:"uri"`
	// MediaType is the optional media type of the file.
	// If not set, the media type is inferred from the file extension.
	MediaType string `json:"mediaType,omitempty"`
	// Digest is a string representing the desired content digest of the file.
	// The following is an example of the contents of Digest:
	//
	//	sha256:7173b809ca12ec5dee4506cd86be934c4596dd234ee82c0662eac04a8c2c71dc
	//
	// This format is equivalent to the format used by the OCI image specification.
	// The digest is optional, but if provided, can be used to verify integrity.
	Digest string `json:"digest,omitempty"`
}
