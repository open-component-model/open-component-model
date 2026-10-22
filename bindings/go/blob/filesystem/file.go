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
// The file is created with 0600 if it does not exist (os.O_CREATE) and truncated if it does (os.O_TRUNC).
// If reading or closing the blob fails, for example on a digest mismatch that a verifying blob reports at EOF
// or on Close, the written regular file is truncated to zero and path is removed if it is a regular file itself,
// so no rejected content is left behind. A symlink at path is kept and its target is left empty.
// Content already written to a named pipe or device cannot be retracted.
// It uses a buffered I/O operation to improve performance, leveraing the internal ioBufPool.
func CopyBlobToOSPath(blob blob.ReadOnlyBlob, path string) error {
	data, err := blob.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get resource data: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to open target file %s: %w", path, err), data.Close())
	}
	// Close data before checking for errors: verifying readers may report a mismatch only on Close.
	if err := errors.Join(copyBuffered(file, data), data.Close()); err != nil {
		fi, statErr := file.Stat()
		regular := statErr == nil && fi.Mode().IsRegular()
		if regular {
			err = errors.Join(err, file.Truncate(0))
		}
		err = errors.Join(err, file.Close())
		if li, lstatErr := os.Lstat(path); regular && lstatErr == nil && li.Mode().IsRegular() {
			err = errors.Join(err, os.Remove(path))
		}
		return err
	}
	return file.Close()
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
