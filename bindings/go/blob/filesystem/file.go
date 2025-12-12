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
// It opens the file in os.O_APPEND mode.
// If the file does not exist, it will be created (os.O_CREATE).
// The function also handles named pipes by setting the appropriate file mode (os.ModeNamedPipe).
// It uses a buffered I/O operation to improve performance, leveraing the internal ioBufPool.
func CopyBlobToOSPath(blob blob.ReadOnlyBlob, path string) (err error) {
	data, err := blob.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get resource data: %w", err)
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()

	var isNamedPipe bool
	fi, err := os.Stat(path)
	if err == nil {
		isNamedPipe = fi.Mode()&os.ModeNamedPipe != 0
	}

	var mode os.FileMode
	if isNamedPipe {
		mode = os.ModeNamedPipe
	} else {
		mode = 0o600
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
	if err != nil {
		return fmt.Errorf("failed to open target file %s: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	buf := ioBufPool.Get().(*[]byte)
	defer ioBufPool.Put(buf)
	if _, err := io.CopyBuffer(file, data, *buf); err != nil {
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
// It uses ensurePathInWorkingDirectory to ensure the path does not escape the working directory.
// The blob is created using the GetBlobFromOSPath function.
// If the path is not absolute, it will be resolved against the working directory.
// If the path is invalid or cannot be resolved, it returns an error.
func GetBlobInWorkingDirectory(path, workingDir string) (*Blob, error) {
	path, err := ensurePathInWorkingDirectory(path, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure path in working directory: %w", err)
	}
	return GetBlobFromOSPath(path)
}

// ensurePathInWorkingDirectory ensures that the given path is resolved against the specified working directory.
// If the path is absolute, it checks if the path is valid within the working directory.
// If the path is relative, it resolves it against the working directory and prevents escaping the working directory.
// If the path cannot be resolved or is invalid, it returns an error.
func ensurePathInWorkingDirectory(path, workingDirectory string) (_ string, err error) {
	if filepath.IsAbs(path) {
		if path, err = filepath.Rel(workingDirectory, path); err != nil {
			return "", fmt.Errorf("failed to create relative path for %q based on working directory %q: %w", path, workingDirectory, err)
		}
	}

	_, err = os.OpenInRoot(workingDirectory, path)
	if err != nil {
		return "", fmt.Errorf("failed to open path %q in root %q: %w", path, workingDirectory, err)
	}

	return filepath.Join(workingDirectory, path), nil
}
