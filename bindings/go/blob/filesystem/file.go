package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
)

const DefaultFileIOBufferSize = 1 << 20 // 1 MiB

// ioBufPool is a pool of byte buffers that can be reused for copying content
// between i/o relevant data, such as files.
var ioBufPool = sync.Pool{
	New: func() interface{} {
		// the buffer size should be larger than or equal to 128 KiB
		// for performance considerations.
		// we choose 1 MiB here so there will be less disk I/O.
		buffer := make([]byte, DefaultFileIOBufferSize)
		return &buffer
	},
}

// CopyBlobToOSPath copies the content of a blob.ReadOnlyBlob to a local path on the operating system's filesystem.
// A regular file is written to a temporary file in the same directory and renamed over path only after the
// content was read completely without error, so a failed copy (for example a digest mismatch reported by a
// verifying blob at EOF) leaves path untouched and an existing file is replaced, not appended to. A replaced
// file keeps its permission bits; a new file is created with 0600.
// A path that exists and is not a regular file, such as a named pipe or a device, is written to directly;
// content already written to it cannot be retracted on error.
// It uses a buffered I/O operation to improve performance, leveraing the internal ioBufPool.
func CopyBlobToOSPath(blob blob.ReadOnlyBlob, path string) (err error) {
	data, err := blob.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get resource data: %w", err)
	}

	fi, statErr := os.Stat(path)
	if statErr == nil && !fi.Mode().IsRegular() {
		defer func() {
			err = errors.Join(err, data.Close())
		}()
		var file *os.File
		if file, err = os.OpenFile(path, os.O_WRONLY, 0); err != nil {
			return fmt.Errorf("failed to open target file %s: %w", path, err)
		}
		defer func() {
			err = errors.Join(err, file.Close())
		}()
		return copyBuffered(file, data)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".ocm-download-*.tmp")
	if err != nil {
		return errors.Join(fmt.Errorf("failed to create temporary file for %s: %w", path, err), data.Close())
	}
	if statErr == nil {
		if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
			return errors.Join(fmt.Errorf("failed to set permissions of %s: %w", path, err),
				data.Close(), tmp.Close(), os.Remove(tmp.Name()))
		}
	}
	// Close data before the rename: verifying readers may report a mismatch only on Close.
	if err := errors.Join(copyBuffered(tmp, data), data.Close(), tmp.Close()); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(fmt.Errorf("failed to move resource data to %s: %w", path, err), os.Remove(tmp.Name()))
	}
	return nil
}

func copyBuffered(dst io.Writer, src io.Reader) error {
	buf := ioBufPool.Get().(*[]byte)
	defer ioBufPool.Put(buf)
	if _, err := io.CopyBuffer(dst, src, *buf); err != nil {
		return fmt.Errorf("failed to copy resource data: %w", err)
	}
	return nil
}

// GetBlobFromOSPath returns a read-only blob that reads from a file on the operating system's filesystem.
func GetBlobFromOSPath(path string) (*Blob, error) {
	return NewFileBlobFromPathWithFlag(path, os.O_RDONLY)
}

// GetBlobInWorkingDirectory returns a blob that reads from the operating system file system,
// ensuring that the path is resolved against the specified working directory.
// It uses EnsurePathInWorkingDirectory to ensure the path does not escape the working directory.
// The blob is created using the GetBlobFromOSPath function.
// If the path is not absolute, it will be resolved against the working directory.
// If the path is invalid or cannot be resolved, it returns an error.
func GetBlobInWorkingDirectory(path, workingDir string) (*Blob, error) {
	path, err := EnsurePathInWorkingDirectory(path, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure path in working directory: %w", err)
	}
	return GetBlobFromOSPath(path)
}

// EnsurePathInWorkingDirectory ensures that the given path is resolved against the specified working directory.
// If the path is absolute, it checks if the path is valid within the working directory.
// If the path is relative, it resolves it against the working directory and prevents escaping the working directory.
// If the path cannot be resolved or is invalid, it returns an error.
func EnsurePathInWorkingDirectory(path, workingDirectory string) (_ string, retErr error) {
	absWorkingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory %q: %w", workingDirectory, err)
	}
	workingDirectory = absWorkingDirectory

	if filepath.IsAbs(path) {
		if path, retErr = filepath.Rel(workingDirectory, path); retErr != nil {
			return "", fmt.Errorf("failed to create relative path for %q based on working directory %q: %w", path, workingDirectory, retErr)
		}
	}

	resolved, err := os.OpenInRoot(workingDirectory, path)
	if err != nil {
		return "", fmt.Errorf("failed to open path %q in root %q: %w", path, workingDirectory, err)
	}
	err = resolved.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close path validation handle for %q in root %q: %w", path, workingDirectory, err)
	}

	return filepath.Join(workingDirectory, path), nil
}
