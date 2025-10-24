package filesystem

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
)

// DefaultTarMediaType is used as blob media type for directories.
const DefaultTarMediaType = "application/x-tar"

// DefaultRawMediaType is used as blob media type for single files.
const DefaultRawMediaType = "application/octet-stream"

// DirOptions contains options for creating a blob from a path.
// This supports both directories (TAR archives) and single files (raw blobs).
type DirOptions struct {
	MediaType    string   // Media type of the resulting blob. If empty, DefaultTarMediaType (directories) or DefaultRawMediaType (single files) is used.
	Compress     bool     // Compress resulting blob using gzip.
	PreserveDir  bool     // Add parent directory to the tar archive (directories only).
	Reproducible bool     // Create a reproducible tar archive (directories only).
	ExcludeFiles []string // List of files to exclude (glob patterns, directories only).
	IncludeFiles []string // List of files to include (glob patterns, directories only).
	WorkingDir   string   // Working directory to ensure the path is within.
}

// GetBlobFromPath creates a blob from a path using the provided options.
// It reads a path, which can either a single file or directory from the filesystem
// and performs additional actions based on options if requested:
//
// - preserves the directory structure
// - adds gzip compression to the resulting blob
// - makes the output reproducible by normalizing file attributes
// - includes or excludes specific files based on patterns
// - checks if the directory is within the working directory
//
// Note on pattern option semantics: include/exclude patterns are matched using
// `filepath.Match` against the full entry path.
// No additional globbing extensions (like `**`) are performed.
// Exclude patterns take precedence over include patterns.
//
// The function returns an error if the file path is empty, the specified path is outside the working directory
// or if there are issues accessing the directory.

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

	// Get file info to determine if it's a directory or single file
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error trying to get FileInfo for path %q: %w", path, err)
	}

	// Route to appropriate handler based on file type
	switch {
	case fi.IsDir():
		return createDirectoryBlob(ctx, path, opt)
	case fi.Mode().IsRegular():
		// Validate that include/exclude patterns are not used for single files
		if len(opt.IncludeFiles) > 0 || len(opt.ExcludeFiles) > 0 {
			return nil, fmt.Errorf("include/exclude patterns are not supported for single files")
		}
		return createSingleFileBlob(path, opt)
	default:
		return nil, fmt.Errorf("unsupported file type %s for path %q", fi.Mode().String(), path)
	}
}

// createDirectoryBlob creates a TAR blob from a directory.
func createDirectoryBlob(ctx context.Context, path string, opt DirOptions) (blob.ReadOnlyBlob, error) {
	var baseDir, subPath string

	// Determine base directory and subpath based on PreserveDir option
	if !opt.PreserveDir {
		baseDir = path
		subPath = "."
	} else {
		baseDir = filepath.Dir(path)
		subPath = filepath.Base(path)
	}

	// Create a virtual filesystem rooted at the base directory
	fileSystem, err := NewFS(baseDir, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual filesystem for path %q: %w", baseDir, err)
	}

	// Create a pipe for streaming TAR data
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		var gerr error

		defer func() {
			// Close tar writer first and combine errors
			if cerr := tw.Close(); cerr != nil {
				if gerr == nil {
					gerr = cerr
				} else {
					gerr = errors.Join(gerr, cerr)
				}
			}
			// Close pipe with combined error (if any, otherwise nil -> io.EOF)
			_ = pw.CloseWithError(gerr)
		}()

		// Create TAR from directory using fs.WalkDir for deterministic ordering
		gerr = createTarFromDirectory(ctx, fileSystem, subPath, opt, tw)
	}()

	// Determine media type
	mediaType := opt.MediaType
	if mediaType == "" {
		mediaType = DefaultTarMediaType
	}

	// Create blob from TAR stream
	var tarBlob blob.ReadOnlyBlob = direct.New(pr, direct.WithMediaType(mediaType))
	if opt.Compress {
		tarBlob = compression.Compress(tarBlob)
	}
	return tarBlob, nil
}

// createSingleFileBlob creates a raw blob from a single file.
func createSingleFileBlob(path string, opt DirOptions) (blob.ReadOnlyBlob, error) {
	// Open the file for reading
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file %q: %w", path, err)
	}

	// Determine media type
	mediaType := opt.MediaType
	if mediaType == "" {
		mediaType = DefaultRawMediaType
	}

	// Create blob from file
	var fileBlob blob.ReadOnlyBlob = direct.New(file, direct.WithMediaType(mediaType))
	if opt.Compress {
		fileBlob = compression.Compress(fileBlob)
	}
	return fileBlob, nil
}

// createTarFromDirectory writes the contents of a directory to the TAR archive using fs.WalkDir for deterministic ordering.
func createTarFromDirectory(ctx context.Context, fileSystem FileSystem, subPath string, opt DirOptions, tw *tar.Writer) error {
	return fs.WalkDir(fileSystem, subPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking directory at path %q: %w", path, err)
		}

		// Check context before processing each entry
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while processing path %q: %w", path, ctx.Err())
		default:
		}

		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("error getting file info for entry %q: %w", path, err)
		}

		// Fail early for symlinks
		if (fi.Mode() & fs.ModeSymlink) != 0 {
			return fmt.Errorf("symlinks are not supported: found symlink %q", path)
		}

		// Check if path is included based on include/exclude patterns
		if ok, err := isPathIncluded(path, opt.IncludeFiles, opt.ExcludeFiles); err != nil {
			return fmt.Errorf("error checking inclusion of path %q: %w", path, err)
		} else if !ok {
			return nil
		}

		// Create TAR header for the entry
		header, err := createTarHeader(fi, "", opt.Reproducible)
		if err != nil {
			return fmt.Errorf("error creating tar header for path %q: %w", path, err)
		}

		// Use cross-platform path separators for TAR archive
		header.Name = filepath.ToSlash(path)

		// Write header to TAR
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header for path %q: %w", path, err)
		}

		// For regular files, copy content to TAR
		if fi.Mode().IsRegular() {
			fr, err := fileSystem.Open(path)
			if err != nil {
				return fmt.Errorf("error opening file %q for reading: %w", path, err)
			}
			defer fr.Close()

			// Check context before potentially long file copy
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while copying file %q: %w", path, ctx.Err())
			default:
			}

			if _, err := io.Copy(tw, fr); err != nil {
				return fmt.Errorf("error copying content of file %q to tar: %w", path, err)
			}
		}

		return nil
	})
}

// isPathIncluded checks if a given path should be included based on include and exclude patterns.
// Simple pattern matching using filepath.Match is used. This means patterns are matched against the entire path, no globbing.
// Exclude patterns take precedence over include patterns.
func isPathIncluded(path string, includePatterns, excludePatterns []string) (bool, error) {
	// Check exclude patterns first, they have precedence.
	for _, pattern := range excludePatterns {
		if ok, err := filepath.Match(pattern, path); err != nil {
			return false, fmt.Errorf("error matching exclude pattern %q with path %q: %w", pattern, path, err)
		} else if ok {
			return false, nil
		}
	}

	// If no include patterns are specified, include by default.
	if len(includePatterns) == 0 {
		return true, nil
	}

	// Check include patterns.
	for _, pattern := range includePatterns {
		if ok, err := filepath.Match(pattern, path); err != nil {
			return false, fmt.Errorf("error matching include pattern %q with path %q: %w", pattern, path, err)
		} else if ok {
			return true, nil
		}
	}

	// If include patterns are specified but none matched, exclude the path.
	return false, nil
}

// createTarHeader creates a TAR header from the given FileInfo.
// If reproducible is true, it normalizes the header for reproducibility.
func createTarHeader(fi fs.FileInfo, linkTarget string, reproducible bool) (*tar.Header, error) {
	h, err := tar.FileInfoHeader(fi, linkTarget)
	if err != nil {
		return nil, fmt.Errorf("error creating tar header for file %q: %w", fi.Name(), err)
	}
	if reproducible {
		h.ModTime = time.Unix(0, 0)
		h.AccessTime = time.Unix(0, 0)
		h.ChangeTime = time.Unix(0, 0)
		h.Uid, h.Gid = 0, 0
		h.Uname, h.Gname = "", ""
		h.Mode &= 0o777 // remove all but permission bits
	}
	return h, nil
}
