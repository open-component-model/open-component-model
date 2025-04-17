package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// File describes an input sourced by a file.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type File struct {
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
