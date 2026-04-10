package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const FileCleanupType = "FileCleanup"

// FileCleanup is a transformer specification to clean up temporary files
// that were buffered to the local filesystem during Get transformations.
// It runs as a final node in the transformation graph after all other
// transformations have completed, removing the temporary buffer files.
// Spec: FileCleanupSpec - the input specification containing the list of files to remove.
// Output: FileCleanupOutput - the output specification reporting how many files were cleaned up.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type FileCleanup struct {
	// +ocm:jsonschema-gen:enum=FileCleanup/v1alpha1
	Type   runtime.Type       `json:"type"`
	ID     string             `json:"id"`
	Spec   *FileCleanupSpec   `json:"spec"`
	Output *FileCleanupOutput `json:"output,omitempty"`
}

// FileCleanupSpec is the input specification for the
// FileCleanup transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileCleanupSpec struct {
	// Files is a list of file access specifications to clean up.
	// Each entry references a file that was buffered during a Get transformation.
	Files []v1alpha1.File `json:"files"`
}

// FileCleanupOutput is the output specification of the
// FileCleanup transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileCleanupOutput struct {
	// CleanedFiles is the number of files that were successfully removed.
	CleanedFiles int `json:"cleanedFiles"`
}
