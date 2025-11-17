package dir

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
)

// GetV1DirBlob creates a ReadOnlyBlob from a v1.Dir specification.
// It reads the directory from the filesystem and applies compression if requested.
// The function returns an error if the file path is empty or if there are issues reading the directory
// contents from the filesystem.
//
// The function is not able to handle symbolic links yet.
//
// The function performs the following steps:
//  1. Validates that the directory path is not empty
//  2. Ensures that the directory path is within the working directory
//     (this is to prevent directory traversal attacks and ensure security)
//  3. Reads the directory contents using an instance of the virtual FileSystem
//  4. Packs the directory contents into a tar archive
//  5. Applies different configuration options of the v1.Dir specification
func GetV1DirBlob(ctx context.Context, dir v1.Dir, workingDirectory string) (blob.ReadOnlyBlob, error) {
	opts := filesystem.DirOptions{
		MediaType:       dir.MediaType,
		Compress:        dir.Compress,
		PreserveDir:     dir.PreserveDir,
		Reproducible:    dir.Reproducible,
		IncludePatterns: dir.IncludeFiles,
		ExcludePatterns: dir.ExcludeFiles,
		WorkingDir:      workingDirectory,
	}
	return filesystem.GetBlobFromPath(ctx, dir.Path, opts)
}
