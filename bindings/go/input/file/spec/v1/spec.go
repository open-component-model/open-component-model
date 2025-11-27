package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// File describes an input sourced by a file.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type File struct {
	// +ocm:jsonschema-gen:enum=file/v1,File/v1
	// +ocm:jsonschema-gen:enum:deprecated=file,File
	Type runtime.Type `json:"type"`
	// Path is the path to the file.
	Path string `json:"path"`
	// MediaType is the media type of the file.
	MediaType string `json:"mediaType,omitempty"`
	// Compress indicates whether the file should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`
}

func (t *File) String() string {
	return t.Path
}

const (
	Version = "v1"
	Type    = "file"
)
