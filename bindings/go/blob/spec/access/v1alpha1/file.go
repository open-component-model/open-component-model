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
	// URI is the URI to the file.
	URI string `json:"uri"`
	// MediaType is the optional media type of the file.
	// If not set, the media type is inferred from the file extension.
	MediaType string `json:"mediaType,omitempty"`
	// Digest allows simple protection of hex formatted digest strings, prefixed
	// by their algorithm. Strings of type Digest have some guarantee of being in
	// the correct format and it provides quick access to the components of a
	// digest string.
	//
	// The following is an example of the contents of Digest types:
	//
	//	sha256:7173b809ca12ec5dee4506cd86be934c4596dd234ee82c0662eac04a8c2c71dc
	//
	// This allows to abstract the digest behind this type and work only in those
	// terms.
	Digest string `json:"digest,omitempty"`
}
