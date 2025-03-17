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
func CopyBlobToOSPath(blob blob.ReadOnlyBlob, path string) error {
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

// GetBlobFromOSPath returns a blob that reads from the operating system file system.
// It creates a new virtual FileSystem instance based on the directory of the provided path.
// The blob is created using the NewFileBlob function, see Blob for details.
func GetBlobFromOSPath(path string) (*Blob, error) {
	fs, err := NewFS(filepath.Dir(path), os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem while trying to access %v: %w", path, err)
	}

	data := NewFileBlob(fs, filepath.Base(path))

	return data, nil
}
