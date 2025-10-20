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
	// Create a TAR stream from the specified path
	reader, err := createTarStream(ctx, path, &opt)
	if err != nil {
		return nil, fmt.Errorf("error creating tar stream from path %q: %w", path, err)
	}

	// Create a ReadOnlyBlob from the TAR archive.
	var tarBlob blob.ReadOnlyBlob = direct.New(reader, direct.WithMediaType(opt.mediaType()))
	if opt.Compress {
		tarBlob = compression.Compress(tarBlob)
	}
	return tarBlob, nil
}

// createTarStream creates a TAR archive from the contents of the specified path, either a directory or a single file.
func createTarStream(ctx context.Context, path string, opt *DirOptions) (io.Reader, error) {

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error stating path %q: %w", path, err)
	}

	var baseDir, subPath string

	// Directory case
	if fi.IsDir() {
		if !opt.PreserveDir {
			baseDir = path
			subPath = "."
			// add parent directory if PreserveDir is set
		} else {
			baseDir = filepath.Dir(path)
			subPath = filepath.Base(path)
		}
		// Single file case
	} else {
		baseDir = filepath.Dir(path)
		subPath = filepath.Base(path)
	}

	// Create a virtual filesystem rooted at the base directory.
	fileSystem, err := NewFS(baseDir, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual filesystem for path %q: %w", baseDir, err)
	}

	// Create a TAR archive from the directory contents.
	// We differentiate between directory and single file cases.
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)

		// use local gerr to capture errors from tar creation
		var gerr error

		// take care to close tar writer and pipe and capture any error
		defer func() { gerr = errors.Join(gerr, tw.Close()) }()
		defer func() { _ = pw.CloseWithError(gerr) }()

		if fi.IsDir() {
			gerr = createTarFromDir(ctx, fileSystem, subPath, opt, tw)
		} else {
			gerr = createTarFromSingleFile(ctx, fileSystem, subPath, opt, tw)
		}
	}()
	return pr, nil
}

// createTarFromDir writes the contents of a directory to the TAR archive.
// subPath is the relative path within the virtual filesystem.
func createTarFromDir(ctx context.Context, fileSystem FileSystem, subPath string, opt *DirOptions, tw *tar.Writer) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while processing directory %q: %w", subPath, ctx.Err())
	default:
	}

	dirEntries, err := fileSystem.ReadDir(subPath)
	if err != nil {
		return fmt.Errorf("error reading directory %q: %w", subPath, err)
	}

	for _, entry := range dirEntries {
		ei, err := entry.Info()
		if err != nil {
			return fmt.Errorf("error getting file info for entry %q in directory %q: %w", entry.Name(), subPath, err)
		}

		// fail early for symlinks
		if (ei.Mode() & fs.ModeSymlink) != 0 {
			return fmt.Errorf("symlinks are not supported: found symlink %q in directory %q", entry.Name(), subPath)
		}

		// reconstruct the full path of the entry
		entryPath := filepath.Join(subPath, entry.Name())
		// Check if path is included based on include/exclude patterns from options.
		ok, err := isPathIncluded(entryPath, opt.IncludeFiles, opt.ExcludeFiles)
		if err != nil {
			return fmt.Errorf("error checking inclusion of entry %q in directory %q: %w", entry.Name(), subPath, err)
		}
		if !ok {
			continue
		}

		// Create TAR header for the entry.
		header, err := createTarHeader(ei, "", opt.Reproducible)
		if err != nil {
			return fmt.Errorf("error creating tar header for entry %q in directory %q: %w", entry.Name(), subPath, err)
		}
		// Set header name to the relative path of the entry with respect to the base directory,
		// to preserve the subfolder structure in the tar archive.
		header.Name = entryPath

		switch {
		case ei.IsDir():
			// Write header to TAR, then recurse into directory.
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("error writing tar header for directory %q: %w", entryPath, err)
			}
			if err := createTarFromDir(ctx, fileSystem, entryPath, opt, tw); err != nil {
				return fmt.Errorf("error creating tar entry %q in directory %q: %w", entryPath, subPath, err)
			}

		case ei.Mode().IsRegular():
			// Write header to TAR.
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("error writing tar header for file %q: %w", entryPath, err)
			}

			// Open file for reading.
			fr, err := fileSystem.Open(entryPath)
			if err != nil {
				return fmt.Errorf("error opening file %q for reading: %w", entryPath, err)
			}

			// Copy file content to TAR. Close immediately after copy; do not defer in loop.
			var copyErr error
			_, copyErr = io.Copy(tw, fr)
			if closeErr := fr.Close(); closeErr != nil {
				copyErr = errors.Join(copyErr, fmt.Errorf("error closing file %q: %w", entryPath, closeErr))
			}
			if copyErr != nil {
				return fmt.Errorf("error copying content of file %q to tar: %w", entryPath, copyErr)
			}

		default:
			return fmt.Errorf("unsupported file type %s for entry %q in directory %q", ei.Mode().String(), entry.Name(), subPath)
		}
	}
	return nil
}

// createTarFromSingleFile writes a single file to the TAR archive.
// subPath is the relative path within the virtual filesystem.
func createTarFromSingleFile(ctx context.Context, fileSystem FileSystem, subPath string, opt *DirOptions, tw *tar.Writer) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while processing single file %q: %w", subPath, ctx.Err())
	default:
	}
	fi, err := fs.Stat(fileSystem, subPath)
	if err != nil {
		return fmt.Errorf("error stating file %q: %w", subPath, err)
	}

	// Create the TAR header for the file.
	header, err := createTarHeader(fi, "", opt.Reproducible)
	if err != nil {
		return fmt.Errorf("error creating tar header for file %q: %w", subPath, err)
	}
	// Use relative path instead of base name of file.
	header.Name = subPath

	// Write the header to the TAR writer.
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("error writing tar header for file %q: %w", subPath, err)
	}

	// Open the file for reading.
	fr, err := fileSystem.Open(subPath)
	if err != nil {
		return fmt.Errorf("error opening file %q for reading: %w", subPath, err)
	}
	defer func() { err = errors.Join(err, fr.Close()) }()

	// Copy the file content to the TAR writer.
	if _, err := io.Copy(tw, fr); err != nil {
		return fmt.Errorf("error copying content of file %q to tar: %w", subPath, err)
	}

	return nil
}

// isPathIncluded checks if a given path should be included based on include and exclude patterns.
// Simple pattern matching using filepath.Match is used. This means patterns are matched against the entire path, no globbing.
// Exclude patterns take precedence over include patterns.
func isPathIncluded(path string, includePatterns, excludePatterns []string) (bool, error) {
	// Check exclude patterns first, they have precedence.
	for _, pattern := range excludePatterns {
		ok, err := filepath.Match(pattern, path)
		if err != nil {
			return false, fmt.Errorf("error matching exclude pattern %q with path %q: %w", pattern, path, err)
		}
		if ok {
			return false, nil
		}
	}

	// If no include patterns are specified, include by default.
	if len(includePatterns) == 0 {
		return true, nil
	}

	// Check include patterns.
	for _, pattern := range includePatterns {
		ok, err := filepath.Match(pattern, path)
		if err != nil {
			return false, fmt.Errorf("error matching include pattern %q with path %q: %w", pattern, path, err)
		}
		if ok {
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
		h.Mode = h.Mode &^ 0o700 // remove all sticky bits and just keep permission bits
	}
	return h, nil
}
