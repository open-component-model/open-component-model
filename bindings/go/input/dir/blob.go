package dir

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
)

// GetV1DirBlob creates a ReadOnlyBlob from a v1.Dir specification.
// It reads the directory from the filesystem and applies compression if requested.
// The function returns an error if the file path is empty or if there are issues reading the directory
// contents from the filesystem.
//
// The function performs the following steps:
//  1. Validates that the directory path is not empty
//  2. Reads the directory contents using an instance of the virtual FileSystem
//  3. Packs the directory contents into a tar archive
//  4. Applies different configuration options of the v1.Dir specification
func GetV1DirBlob(dir v1.Dir) (blob.ReadOnlyBlob, error) {
	// TODO:
	// - Handle FollowSymlinks option.

	// Pack directory contents as a tar archive.
	reader, err := packDirToTar(dir.Path, &dir)
	if err != nil {
		return nil, fmt.Errorf("error producing blob for a dir input: %w", err)
	}

	// Wrap the tar archive in a ReadOnlyBlob.
	var dirBlob blob.ReadOnlyBlob = inmemory.New(reader, inmemory.WithMediaType(dir.MediaType))

	// gzip the blob, if requested in the spec.
	if dir.Compress {
		dirBlob = compression.Compress(dirBlob)
	}

	return dirBlob, nil
}

// packDirToTar is the main function, which creates a tar archive from the contents of the specified directory.
// It creates an instance of the virtual FileSystem based on the directory path, creates a tar writer and
// triggers recursive packaging of the directory contents.
func packDirToTar(path string, opt *v1.Dir) (_ io.Reader, err error) {
	if path == "" {
		return nil, fmt.Errorf("dir path must not be empty")
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
	fs, err := filesystem.NewFS(baseDir, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem while trying to access %v: %w", path, err)
	}

	// Create a new tar writer.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	defer func() {
		err = errors.Join(err, tw.Close())
	}()

	// Walk recursively through directory contents and add it to the tar.
	err = walkDirContents(subDir, baseDir, opt, fs, tw)
	if err != nil {
		return nil, fmt.Errorf("failed to package directory contents as a tar archive: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}

// walkDirContents does recursive packaging of the directory contents, while keeping the subfolder structure.
// The function goes the directory contents file by file, checks if it should be included or excluded,
// creates tar headers for each file and subfolder, and writes the file contents to the tar archive.
// For subdirectories it calls itself recursively to process the subfolder contents.
func walkDirContents(currentDir string, baseDir string,
	opt *v1.Dir, fs filesystem.FileSystem, tw *tar.Writer,
) (err error) {
	// Read directory contents.
	dirEntries, err := fs.ReadDir(currentDir)
	if err != nil {
		return fmt.Errorf("failed to read directory entries for dirictory %s: %w", currentDir, err)
	}

	// Iterate over directory entries.
	for _, entry := range dirEntries {
		// Get FileInfo for the entry.
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to get information for file %s: %w", entry.Name(), err)
		}

		// Construct the relative path of the entry with respect to the base directory.
		entryPath := filepath.Join(currentDir, entry.Name())
		// Check, if the entry should be included in the tar archive.
		include, err := isPathIncluded(entryPath, opt.ExcludeFiles, opt.IncludeFiles)
		if err != nil {
			return fmt.Errorf("failed to check if entry %s should be included in the tar archive: %w", entryPath, err)
		}
		if !include {
			continue
		}

		// Create tar header.
		header, err := reproducibleTarHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header for file %s: %w", info.Name(), err)
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
			file, err := fs.OpenFile(entryPath, os.O_RDONLY, 0o644)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", entryPath, err)
			}
			defer func() {
				err = errors.Join(err, file.Close())
			}()

			if _, err := io.Copy(tw, file); err != nil {
				return fmt.Errorf("failed to write file contents to tar archive: %w", err)
			}

		case entry.IsDir():
			// The entry is a subdirectory.
			// Write the header to the tar archive.
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header to tar archive: %w", err)
			}

			// Process subdirectory contents.
			if err := walkDirContents(entryPath, baseDir, opt, fs, tw); err != nil {
				return err
			}

		case header.Typeflag == tar.TypeSymlink:
			/*// The entry is a symlink.
			if !opt.FollowSymlinks {
				absPath := filepath.Join(baseDir, entryPath)
				link, err := fs.Readlink(absPath)
				if err != nil {
					return fmt.Errorf("cannot read symlink %s: %w", absPath, err)
				}
				header.Linkname = link
				// Write the header to the tar archive.
				if err := tw.WriteHeader(header); err != nil {
					return fmt.Errorf("failed to write tar header to tar archive: %w", err)
				}
			} else {
			}*/
			return fmt.Errorf("symlinks not supported yet")

		default:
			return fmt.Errorf("unsupported file type %s of %s", info.Mode().String(), entryPath)
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

// reproducibleTarHeader creates a tar header with certain fields normalized.
// The function relies on tar.FileInfoHeader() for header creation and keeps the other fields intact.
func reproducibleTarHeader(fi fs.FileInfo, link string) (*tar.Header, error) {
	h, err := tar.FileInfoHeader(fi, link)
	if err != nil {
		return nil, fmt.Errorf("failed to create tar header for file %s: %w", fi.Name(), err)
	}

	// Normalize header fields to ensure reproducibility.
	timestamp := time.Unix(0, 0).UTC()
	h.Mode = int64(fs.ModePerm) // Full permissions for everyone.
	h.Uid = 0                   // Root user.
	h.Gid = 0                   // Root group.
	h.Uname = "root"
	h.Gname = "root"
	h.ModTime = timestamp
	h.AccessTime = timestamp
	h.ChangeTime = timestamp

	// Clear system-specific fields
	h.Xattrs = nil //nolint:staticcheck // SA1019: tar.FileInfoHeader() still sets h.Xattrs, so it needs to be cleared here.
	h.PAXRecords = nil
	h.Devmajor = 0
	h.Devminor = 0

	return h, nil
}
