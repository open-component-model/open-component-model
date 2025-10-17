package filesystem

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
)

// DefaultTarMediaType is used as blob media type, if the MediaType field is not set in the DirOptions.
const DefaultTarMediaType = "application/x-tar"

// DirOptions contains options for creating a blob from a path.
// This is very similar to v1.Dir specification in the input/dir package.
type DirOptions struct {
	MediaType    string   // Media type of the resulting blob. If empty, DefaultTarMediaType is used.
	Compress     bool     // Compress resulting blob using gzip.
	PreserveDir  bool     // Add parent directory to the tar archive.
	Reproducible bool     // Create a reproducible tar archive (fixed timestamps, uid/gid etc).
	ExcludeFiles []string // List of files to exclude (glob patterns).
	IncludeFiles []string // List of files to include (glob patterns).
	WorkingDir   string   // Working directory to ensure the path is within.
}

// mediaType returns the media type to be used for the blob.
// If MediaType is not set, it returns the DefaultTarMediaType.
func (o *DirOptions) mediaType() string {
	if o.MediaType == "" {
		return DefaultTarMediaType
	}
	return o.MediaType
}

// GetBlobFromPath creates a blob from a path using the provided options.
//
// It reads a path from the filesystem and offers include and exclude filters.
// Matching files are added to a TAR archive, and if configured also the parent directory.
// If configured, the final blob gets compressed using gzip.
//
// The function returns an error if the file path is empty, the specified path is outside the working directory
// or if there are issues reading the directory.

func GetBlobFromPath(ctx context.Context, path string, opt DirOptions) (blob.ReadOnlyBlob, error) {

	// Validate the input path
	if path == "" {
		return nil, fmt.Errorf("path must not be empty")
	}

	// Ensure the path is within the working directory if specified
	if opt.WorkingDir != "" {
		if _, err := EnsurePathInWorkingDirectory(path, opt.WorkingDir); err != nil {
			return nil, fmt.Errorf("error ensuring path %q in working directory %q: %w", path, opt.WorkingDir, err)
		}
	}

	// Check if path is a directory or just a single file
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error stating path %q: %w", path, err)
	}

	base := path
	sub := "."

	// Directory handling
	if fileInfo.IsDir() {
		// If PreserveDir is set, adjust base and sub path to also include the parent directory.
		if opt.PreserveDir {
			base = filepath.Dir(path)
			sub = filepath.Base(path)
		}
	} else {
		// File handling
		base = filepath.Dir(path)
	}

	// Create a virtual filesystem rooted at the base directory.
	fsystem, err := NewFS(base, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual filesystem for path %q: %w", base, err)
	}

	// Create a TAR archive from the directory contents.
	// We use a pipe and a goroutine to create the TAR.
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)

		// use local gerr to capture errors from tar creation
		var gerr error

		// take care to close the tar writer and capture any error
		defer func() {
			gerr = errors.Join(gerr, tw.Close())
		}()

		// take care to close the pipe writer and hand over all errors
		defer func() {
			_ = pw.CloseWithError(gerr)
		}()

		gerr = createTarFromPath(ctx, fsystem, sub, &opt, tw)
	}()

	// Create a ReadOnlyBlob from the TAR archive.
	var tarBlob blob.ReadOnlyBlob = direct.New(pr, direct.WithMediaType(opt.mediaType()))

	// If requested, compress blob (using gzip).
	if opt.Compress {
		tarBlob = compression.Compress(tarBlob)
	}

	return tarBlob, nil
}

// createTarFromPath creates a TAR archive from the contents of the specified path.
func createTarFromPath(ctx context.Context, fs FileSystem, path string, opt *DirOptions, tw *tar.Writer) error {

	return nil
}
