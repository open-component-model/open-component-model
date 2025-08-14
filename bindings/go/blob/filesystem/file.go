package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
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
