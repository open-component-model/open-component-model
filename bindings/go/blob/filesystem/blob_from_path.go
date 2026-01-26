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
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
)

// DirOptions contains options for creating a blob from a path.
type DirOptions struct {
	MediaType       string   // Media type of the resulting blob. If empty, defaults are used.
	Compress        bool     // Compress resulting blob using gzip.
	PreserveDir     bool     // Add parent directory to the tar archive.
	Reproducible    bool     // Create a reproducible tar archive (fixed timestamps, uid/gid etc).
	ExcludePatterns []string // Patterns to exclude (glob patterns). Applies to files and directories.
	IncludePatterns []string // Patterns to include (glob patterns). Applies to files and directories.
	WorkingDir      string   // Working directory to ensure the path is within and avoid path traversal.
}

// DefaultTarMediaType is used as blob media type for directories, if not set in the DirOptions.
const DefaultTarMediaType = "application/x-tar"

// DefaultFileMediaType is used as blob media type for a single file, if not set in the DirOptions.
const DefaultFileMediaType = "application/octet-stream"

// GetBlobFromPath creates a blob from a path using the provided options.
//
// Directories are added as TAR files, single files are added as-is.
// If configured, the final blob gets compressed using gzip.
// Exclude and include patterns can be used to filter files when adding directories.
//
// Note on pattern option semantics:
// Include/exclude patterns are matched using `filepath.Match`
// No additional globbing extensions (like `**`) are performed.
// Exclude patterns take precedence over include patterns.
// Patterns are not supported for single file paths and will result in an error.
//
// Paths and patterns are normalized to use forward slashes (`/`) as separators for matching
// and have any leading `./` or `/` removed.
// Symlinks are not supported so far and will result in an error.
func GetBlobFromPath(ctx context.Context, path string, opt DirOptions) (blob.ReadOnlyBlob, error) {
	// Validate the input path
	if path == "" {
		return nil, fmt.Errorf("path must not be empty")
	}

	// Ensure the path is within the working directory if specified
	// Provides full path-traversal protection
	if opt.WorkingDir != "" {
		if _, err := ensurePathInWorkingDirectory(path, opt.WorkingDir); err != nil {
			return nil, fmt.Errorf("error ensuring path %q in working directory %q: %w", path, opt.WorkingDir, err)
		}
	}

	// Check if path is a valid directory or single file.
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error accessing path %q: %w", path, err)
	}

	// Handle directory or single file cases
	switch {
	case fi.IsDir():
		// Create blob from directory using TAR archive
		return createDirBlob(ctx, path, opt)
	case fi.Mode().IsRegular():
		// Create blob with single file
		return createSingleFileBlob(path, opt)
	default:
		// Handle unsupported file types (symlinks, etc.)
		return nil, fmt.Errorf("unsupported file type %s for path %q", fi.Mode().String(), path)
	}
}

// createDirBlob creates a TAR archive blob from the contents of the specified directory.
// If requested it compresses the resulting blob using gzip.
// It uses a virtual filesystem to read the directory contents and streams the TAR data using a pipe.
func createDirBlob(ctx context.Context, path string, opt DirOptions) (blob.ReadOnlyBlob, error) {
	// Determine filesystem root and start dir walk based on PreserveDir
	baseFSPath := path
	subPath := "."
	if opt.PreserveDir {
		// Root the virtual FS at the parent, and start walking at the preserved base directory name.
		baseFSPath = filepath.Dir(path)
		subPath = filepath.Base(path)
	}

	// Create a virtual filesystem rooted at baseFSPath
	fileSystem, err := NewFS(baseFSPath, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual filesystem for path %q: %w", baseFSPath, err)
	}

	// Create TAR stream using pipe
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)

		// Track any error from TAR creation
		var tarErr error

		defer func() {
			// Close TAR writer
			closeErr := tw.Close()
			// Close pipe with combined error (if any, otherwise nil -> io.EOF)
			_ = pw.CloseWithError(errors.Join(tarErr, closeErr))
		}()

		// If context is already done, abort early.
		select {
		case <-ctx.Done():
			tarErr = fmt.Errorf("context cancelled while preparing tar for path %q: %w", path, ctx.Err())
			return
		default:
		}

		// Create TAR content from directory
		tarErr = createTarFromDir(ctx, fileSystem, subPath, opt, tw)
	}()

	// Set media type for TAR blob
	mediaType := DefaultTarMediaType
	if opt.MediaType != "" {
		mediaType = opt.MediaType
	}

	// Create ReadOnlyBlob from TAR stream
	var tarBlob blob.ReadOnlyBlob = direct.New(pr, direct.WithMediaType(mediaType))

	// Apply compression if requested
	if opt.Compress {
		tarBlob = compression.Compress(tarBlob)
	}
	return tarBlob, nil
}

// createSingleFileBlob creates a blob from the specified single file.
func createSingleFileBlob(path string, opt DirOptions) (blob.ReadOnlyBlob, error) {
	// Validate that include/exclude patterns are not used for single files
	if len(opt.IncludePatterns) > 0 || len(opt.ExcludePatterns) > 0 {
		return nil, fmt.Errorf("include/exclude patterns are not supported for single files")
	}

	// Open file for reading
	fr, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening single file %q for reading: %w", path, err)
	}

	// Determine media type for single file
	mediaType := DefaultFileMediaType
	if opt.MediaType != "" {
		mediaType = opt.MediaType
	}

	// Create ReadOnlyBlob from file
	var fileBlob blob.ReadOnlyBlob = direct.New(fr, direct.WithMediaType(mediaType))
	// Apply compression if requested
	if opt.Compress {
		fileBlob = compression.Compress(fileBlob)
	}
	return fileBlob, nil
}

// createTarFromDir creates a TAR archive from the filesystem.
// Uses the virtual filesystem to read the directory contents.
// Uses fs.WalkDir to traverse the directory structure in a deterministic order.
// This is required to ensure reproducible TAR archives.
func createTarFromDir(ctx context.Context, fileSystem FileSystem, subPath string, opt DirOptions, tw *tar.Writer) error {
	return fs.WalkDir(fileSystem, subPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %q: %w", path, err)
		}

		// Check context before processing each entry
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while processing %q: %w", path, ctx.Err())
		default:
		}

		// Get file info
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("error getting file info for %q: %w", path, err)
		}

		// Reject symlinks
		if (fi.Mode() & fs.ModeSymlink) != 0 {
			return fmt.Errorf("symlinks are not supported yet: found symlink %q", path)
		}

		// Process directory or file
		if fi.IsDir() {
			return processDirectory(path, fi, opt, tw)
		}
		return processFile(path, fi, fileSystem, opt, tw)
	})
}

// processDirectory handles directory entries during DirWalk
// Prune on exclude, optionally write header if included, always traverse
func processDirectory(path string, fi fs.FileInfo, opt DirOptions, tw *tar.Writer) error {
	// Exclude precedence: if directory matches any exclude pattern -> prune subtree by skipping dir
	if len(opt.ExcludePatterns) > 0 {
		inc, err := isPathIncluded(path, nil, opt.ExcludePatterns)
		if err != nil {
			return fmt.Errorf("error checking exclusion of directory %q: %w", path, err)
		}
		if !inc {
			return fs.SkipDir
		}
	}

	// Write directory header only if the directory path itself is included
	inc, err := isPathIncluded(path, opt.IncludePatterns, opt.ExcludePatterns)
	if err != nil {
		return fmt.Errorf("error checking include/exclude pattern for directory %q: %w", path, err)
	}
	if inc {
		header, err := createTarHeader(fi, "", opt.Reproducible)
		if err != nil {
			return fmt.Errorf("error creating tar header for directory %q: %w", path, err)
		}
		name := filepath.ToSlash(path)
		if !strings.HasSuffix(name, "/") {
			name += "/"
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header for directory %q: %w", path, err)
		}
	}
	// continue walking into directory in any case
	return nil
}

// processFile handles file entries during DirWalk
// Write only if included (exclude precedence handled in isPathIncluded)
func processFile(path string, fi fs.FileInfo, fileSystem FileSystem, opt DirOptions, tw *tar.Writer) error {
	inc, err := isPathIncluded(path, opt.IncludePatterns, opt.ExcludePatterns)
	if err != nil {
		return fmt.Errorf("error checking include/exclude pattern for file %q: %w", path, err)
	}
	if !inc {
		return nil
	}

	header, err := createTarHeader(fi, "", opt.Reproducible)
	if err != nil {
		return fmt.Errorf("error creating tar header for %q: %w", path, err)
	}
	header.Name = filepath.ToSlash(path)

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("error writing tar header for %q: %w", path, err)
	}

	fr, err := fileSystem.Open(path)
	if err != nil {
		return fmt.Errorf("error opening file %q for reading: %w", path, err)
	}

	_, copyErr := io.CopyN(tw, fr, header.Size)
	closeErr := fr.Close()

	if copyErr != nil {
		return fmt.Errorf("error copying content of file %q to tar: %w", path, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("error closing file %q: %w", path, closeErr)
	}

	return nil
}

// isPathIncluded checks if a given path should be included based on include and exclude patterns.
// Simple pattern matching using filepath.Match is used. Patterns are matched against the entire path, no globbing.
// Exclude patterns take precedence over include patterns and apply to both files and directories.
func isPathIncluded(path string, includePatterns, excludePatterns []string) (bool, error) {
	// Normalize path for consistent matching.
	np := normalizePathOrPattern(path)

	// Exclude patterns take precedence.
	for _, pattern := range excludePatterns {
		pp := normalizePathOrPattern(pattern)
		if ok, err := filepath.Match(pp, np); err != nil {
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
		pp := normalizePathOrPattern(pattern)
		if ok, err := filepath.Match(pp, np); err != nil {
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

// normalizePathOrPattern normalizes a path or pattern for matching:
// Convert to forward slashes and strip leading "./" and "/"
func normalizePathOrPattern(s string) string {
	s = filepath.ToSlash(s)
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	return s
}
