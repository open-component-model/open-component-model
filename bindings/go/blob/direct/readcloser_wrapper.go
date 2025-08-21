package direct

import (
	"io"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
)

// readCloserWrapper wraps a ReadOnlyBlob to provide an io.ReadCloser that
// opens the underlying blob on the first read and automatically closes it.
type readCloserWrapper struct {
	blob   blob.ReadOnlyBlob
	reader io.ReadCloser
	mu     sync.Mutex
	opened bool
	closed bool
}

// NewReadCloserWrapper creates an io.ReadCloser wrapper around a ReadOnlyBlob.
// The wrapper will open the underlying blob on the first read operation and
// close it when Close() is called.
func NewReadCloserWrapper(b blob.ReadOnlyBlob) io.ReadCloser {
	return &readCloserWrapper{
		blob: b,
	}
}

// Read implements io.Reader. It opens the underlying blob on the first call
// and delegates all subsequent reads to the opened reader.
func (w *readCloserWrapper) Read(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, io.EOF
	}

	// Open the blob reader on the first read
	if !w.opened {
		reader, err := w.blob.ReadCloser()
		if err != nil {
			return 0, err
		}
		w.reader = reader
		w.opened = true
	}

	return w.reader.Read(p)
}

// Close implements io.Closer. It closes the underlying blob reader if it was opened.
// Subsequent calls to Read() will return io.ErrClosedPipe.
func (w *readCloserWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	if w.opened && w.reader != nil {
		return w.reader.Close()
	}

	return nil
}
