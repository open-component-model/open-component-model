package blob

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// DefaultTarMediaType is used as blob media type, if the MediaType field is not set in the DirOptions.
const DefaultTarMediaType = "application/x-tar"

// DirOptions contains options for creating a blob from a directory.
// This is very similar to v1.Dir specification in the input/dir package.
type DirOptions struct {
	MediaType    string
	Compress     bool
	PreserveDir  bool
	Reproducible bool
	ExcludeFiles []string
	IncludeFiles []string
}

// mediaType returns the media type to be used for the blob.
// If MediaType is not set, it returns the DefaultTarMediaType.
func (o *DirOptions) mediaType() string {
	if o.MediaType == "" {
		return DefaultTarMediaType
	}
	return o.MediaType
}

// GetBlobFromDir creates a blob from a directory using the provided options.
//
// It reads a directory from the filesystem based on specified include and exclude filters.
// All files (and if configured the parent directory) are added to a TAR archive, which is then added to a blob
// If configured, the final blob gets compressed using gzip.
//
// The function returns an error if the file path is empty, the specified path is outside the working directory
// or if there are issues reading the directory.

func GetBlobFromDir(ctx context.Context, path string, workingDirectory string, opt DirOptions) (ReadOnlyBlob, error) {

	// Validate the input path
	if path == "" {
		return nil, fmt.Errorf("path must not be empty")
	}

	// Validate the working directory
	if workingDirectory == "" {
		return nil, fmt.Errorf("workingDirectory must not be empty")
	}

	// Ensure the path is within the working directory
	if _, err := filesystem.EnsurePathInWorkingDirectory(path, workingDirectory); err != nil {
		return nil, fmt.Errorf("error ensuring path %q in working directory %q: %w", path, workingDirectory, err)
	}

	// Prepare the virtual filesystem.
	// If PreserveDir is set, adjust base and sub path to also include the parent directory.
	base := path
	sub := "."
	if opt.PreserveDir {
		base = filepath.Dir(path)
		sub = filepath.Base(path)
	}

	// Create a virtual filesystem rooted at the base directory.
	fs, err := filesystem.NewFS(base, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual filesystem for path %q: %w", base, err)
	}

	// Create a TAR archive from the directory contents.
	// We use a pipe and a goroutine to create the TAR.
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		var err error

		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic during creation of tar from dir %q: %v", path, r)
			}
			err = errors.Join(err, tw.Close())
			_ = pw.CloseWithError(err)
		}()

		err = createTarFromDir(ctx, fs, sub, &opt, tw)
	}()

	// Create a ReadOnlyBlob from the TAR archive.
	var dirBlob ReadOnlyBlob = direct.New(pr, direct.WithMediaType(opt.mediaType()))

	// If requested, compress blob (using gzip).
	if opt.Compress {
		dirBlob = compression.Compress(dirBlob)
	}

	return dirBlob, nil

}

// GetBlobFromDir
func createTarFromDir(ctx context.Context, fs filesystem.FileSystem, dir string, opt *DirOptions, tw *tar.Writer) error {

}
