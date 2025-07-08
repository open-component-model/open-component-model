package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Dir describes an input sourced by a directory.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Dir struct {
	Type runtime.Type `json:"type"`

	// Path is the path to the directory.
	Path string `json:"path"`

	// MediaType is the media type of the input (resulting blob).
	MediaType string `json:"mediaType,omitempty"`

	// Compress indicates whether the resulting blob should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`

	// PreserveDir defines that the directory specified in the Path field should be included in the resulting blob.
	PreserveDir bool `json:"preserveDir,omitempty"`

	// FollowSymlinks will include the content of the encountered symbolic links to the resulting blob.
	FollowSymlinks bool `json:"followSymlinks,omitempty"`

	// ExcludeFiles is a list of file name patterns to exclude from addition to the resulting blob.
	// Excluded files always override included files.
	ExcludeFiles []string `json:"excludeFiles,omitempty"`

	// IncludeFiles is a list of file name patterns to exclusively add to the resulting blob.
	IncludeFiles []string `json:"includeFiles,omitempty"`
}

func (t *Dir) String() string {
	return t.Path
}

const (
	Version = "v1"
	Type    = "dir"
)
