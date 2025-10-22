package dir

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
)

// GetV1DirBlob creates a ReadOnlyBlob from a v1.Dir specification using filesystem.GetBLobFromPath()
// It reads the directory from the filesystem and performs additional actions if requested:
// - sets media type
// - preserves the directory structure
// - adds gzip compression
// - makes the output reproducible by normalizing file attributes
// - includes or excludes specific files based on patterns
// - checks if the directory is within the working directory
//
// The function is not able to handle symbolic links yet.
func GetV1DirBlob(ctx context.Context, dir v1.Dir, workingDirectory string) (blob.ReadOnlyBlob, error) {
	// Convert v1.Dir to DirOptions
	options := filesystem.DirOptions{
		MediaType:    dir.MediaType,
		Compress:     dir.Compress,
		PreserveDir:  dir.PreserveDir,
		Reproducible: dir.Reproducible,
		ExcludeFiles: dir.ExcludeFiles,
		IncludeFiles: dir.IncludeFiles,
		WorkingDir:   workingDirectory,
	}

	return filesystem.GetBlobFromPath(ctx, dir.Path, options)
}
