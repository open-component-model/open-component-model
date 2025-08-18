package dir

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
)

// DEFAULT_TAR_MIME_TYPE is used as blob media type, if the MediaType field is not set in the spec.
const DEFAULT_TAR_MIME_TYPE = "application/x-tar"

var ErrEmptyPath = errors.New("dir path must not be empty")

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
	// Pack directory contents as a tar archive.

	if _, err := filesystem.EnsurePathInWorkingDirectory(dir.Path, workingDirectory); err != nil {
		return nil, fmt.Errorf("error ensuring path %q in working directory %q: %w", dir.Path, workingDirectory, err)
	}

	reader, err := packDirToTar(ctx, dir.Path, &dir)
	if err != nil {
		return nil, fmt.Errorf("error producing blob for a dir input: %w", err)
	}

	// Wrap the tar archive in a ReadOnlyBlob.
	mediaType := dir.MediaType
	if mediaType == "" {
		mediaType = DEFAULT_TAR_MIME_TYPE
	}

	var dirBlob blob.ReadOnlyBlob = direct.New(reader, direct.WithMediaType(mediaType))

	// gzip the blob, if requested in the spec.
	if dir.Compress {
		dirBlob = compression.Compress(dirBlob)
	}

	return dirBlob, nil
}

// packDirToTar is the main function, which creates a tar archive from the contents of the specified directory.
// It creates an instance of the virtual FileSystem based on the directory path, creates a tar writer and
// triggers recursive packaging of the directory contents.
func packDirToTar(ctx context.Context, path string, opt *v1.Dir) (io.Reader, error) {
	if path == "" {
		return nil, ErrEmptyPath
	}

	// Determine the base directory for relative paths in the tar archive.
	baseDir := path
	subDir := ""
	if opt.PreserveDir {
		// PreserveDir defines that the directory specified in the path field should be included in the blob.
		baseDir = filepath.Dir(path)
		subDir = filepath.Base(path)
	}

	// Create a new virtual FileSystem instance based on the provided directory path.
	fileSystem, err := filesystem.NewFS(baseDir, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem while trying to access %v: %w", path, err)
	}

	// Create a pipe for streaming the tar data
	pr, pw := io.Pipe()

	// Start a goroutine to create the tar and write the data to the pipe.
	go func() {
		// Create tar writer
		tw := tar.NewWriter(pw)

		// Walk recursively through directory contents and add it to the tar.
		err = walkDirContents(ctx, subDir, baseDir, opt, fileSystem, tw)

		// Close tar writer.
		err = errors.Join(err, tw.Close())

		// Close PipeWriter with CloseWithError():
		// - If the input parameter is nil, the pipe is just normally closed without an error.
		// - The return value is always nil, i.e. can be ignored.
		_ = pw.CloseWithError(err)
	}()

	if err != nil {
		return nil, fmt.Errorf("failed to package directory %q as a tar archive: %w", path, err)
	}

	return pr, nil
}

// walkDirContents does recursive packaging of the directory contents, while keeping the subfolder structure.
// The function goes the directory contents file by file, checks if it should be included or excluded,
// creates tar headers for each file and subfolder, and writes the file contents to the tar archive.
// For subdirectories it calls itself recursively to process the subfolder contents.
// TODO(ikhandamirov): limit recursion depth to avoid stack overflow.
func walkDirContents(ctx context.Context, currentDir string, baseDir string,
	opt *v1.Dir, fileSystem filesystem.FileSystem, tw *tar.Writer,
) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while processing directory %q: %w", currentDir, ctx.Err())
	default:
	}

	// Read directory contents.
	dirEntries, err := fileSystem.ReadDir(currentDir)
	if err != nil {
		return fmt.Errorf("failed to read directory entries for directory %q: %w", currentDir, err)
	}

	// Iterate over directory entries.
	for _, entry := range dirEntries {
		// Get FileInfo for the entry.
		fi, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to get information for file %q: %w", entry.Name(), err)
		}

		// Construct the relative path of the entry with respect to the base directory.
		entryPath := filepath.Join(currentDir, entry.Name())
		// Check, if the entry should be included in the tar archive.
		include, err := isPathIncluded(entryPath, opt.ExcludeFiles, opt.IncludeFiles)
		if err != nil {
			return fmt.Errorf("failed to check if entry %q should be included in the tar archive: %w", entryPath, err)
		}
		if !include {
			continue
		}

		// Create tar header.
		header, err := createTarHeader(fi, "", opt.Reproducible)
		if err != nil {
			return fmt.Errorf("failed to create tar header for file %q: %w", fi.Name(), err)
		}
		// Set header name to the relative path of the entry with respect to the base directory,
		// to preserve the subfolder structure in the tar archive.
		header.Name = entryPath

		switch {
		case entry.Type().IsRegular():
			// The entry is a regular file.
			// Write the header to the tar archive.
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header to tar archive: %w", err)
			}

			// Copy file content to the tar archive.
			file, err := fileSystem.OpenFile(entryPath, os.O_RDONLY, 0o644)
			if err != nil {
				return fmt.Errorf("failed to open file %q: %w", entryPath, err)
			}
			_, err = io.CopyN(tw, file, header.Size)
			if err != nil {
				err = errors.Join(err, file.Close())
				return fmt.Errorf("failed to write file %q to tar archive: %w", entryPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("unable to close file %q: %w", entryPath, err)
			}

		case entry.IsDir():
			// The entry is a subdirectory.
			// Write the header to the tar archive.
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header to tar archive: %w", err)
			}

			// Process subdirectory contents.
			if err := walkDirContents(ctx, entryPath, baseDir, opt, fileSystem, tw); err != nil {
				return err
			}

		case header.Typeflag == tar.TypeSymlink:
			// TODO(ikhandamirov): add support for symlinks, if there is stakeholder demand.
			// Until then it won't be possible to package directories containing symlinks.
			return fmt.Errorf("symbolic link %q encountered, symlinks not supported yet", entryPath)

		default:
			return fmt.Errorf("unsupported file type %q of file %q", fi.Mode().String(), entryPath)
		}
	}

	return nil
}

// isPathIncluded determines whether a file system entry should be included into blob.
// Note that it relies on standard Go "path/filepath.Match()" method, which is rather limited,
// when it comes to pattern matching.
func isPathIncluded(path string, excludePatterns, includePatterns []string) (bool, error) {
	// First check, if one of exclude regex matches.
	for _, ex := range excludePatterns {
		match, err := filepath.Match(ex, path)
		if err != nil {
			return false, fmt.Errorf("failed to match path to exclude pattern %w", err)
		}
		if match {
			return false, nil
		}
	}

	// If no explicit includes are defined, include everything.
	if len(includePatterns) == 0 {
		return true, nil
	}

	// Otherwise check if the include regex match.
	for _, in := range includePatterns {
		match, err := filepath.Match(in, path)
		if err != nil {
			return false, fmt.Errorf("failed to match path to include pattern %w", err)
		}
		if match {
			return true, nil
		}
	}

	// Finally return false if no include pattern matched.
	return false, nil
}

// createTarHeader creates a tar header.
// If reproducible is set to false, the header is completely generated by tar.FileInfoHeader().
// If reproducible is set to true, only essential fields keep the original values,
// all the other fields are normalized.
func createTarHeader(fi fs.FileInfo, link string, reproducible bool) (*tar.Header, error) {
	h, err := tar.FileInfoHeader(fi, link)
	if err != nil {
		return nil, fmt.Errorf("failed to create tar header for file %q: %w", fi.Name(), err)
	}

	if reproducible {
		h = &tar.Header{
			Typeflag: h.Typeflag,
			Name:     h.Name,
			Size:     h.Size,
			Linkname: h.Linkname,
			Mode:     int64(fs.ModePerm), // Full permissions for everyone.
		}
	}

	return h, nil
}
