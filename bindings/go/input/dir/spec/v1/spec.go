package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Dir describes an input sourced by a directory.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Dir struct {
	// +ocm:jsonschema-gen:enum=dir/v1,Dir/v1
	// +ocm:jsonschema-gen:enum:deprecated=dir,Dir
	Type runtime.Type `json:"type"`

	// Path is the path to the directory.
	Path string `json:"path"`

	// MediaType is the media type of the resulting blob.
	MediaType string `json:"mediaType,omitempty"`

	// Compress indicates whether the resulting blob should be compressed with gzip.
	Compress bool `json:"compress,omitempty"`

	// PreserveDir defines that the directory specified in the Path field should be included in the resulting blob.
	PreserveDir bool `json:"preserveDir,omitempty"`

	// FollowSymlinks will include the content of the encountered symbolic links to the resulting blob.
	// Support for this option is not implemented yet. The field is included for compatibility with previous OCM version.
	FollowSymlinks bool `json:"followSymlinks,omitempty"`

	// ExcludeFiles is a list of file name patterns to exclude from addition to the resulting blob.
	// Excluded files always override included files.
	ExcludeFiles []string `json:"excludeFiles,omitempty"`

	// IncludeFiles is a list of file name patterns to exclusively add to the resulting blob.
	IncludeFiles []string `json:"includeFiles,omitempty"`

	// Reproducible defines that the attributes of the included files have to be normalized.
	// This is important if reproducible generation of blobs is required. In this case the blobs
	// need to be comparable on byte level (e.g. for hashing). So, if Reproducible is set to true,
	// to get fully byte-equivalent blobs despite different file modification time, permission bits, etc.,
	// these attributes will be set to fixed values while creating the blob.
	Reproducible bool `json:"reproducible,omitempty"`
}

func (t *Dir) String() string {
	return t.Path
}

const (
	Version = "v1"
	Type    = "dir"
)
