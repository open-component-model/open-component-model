package download

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// Blob is the file-backed blob returned by [Download]. The file behind it is a
// temporary download artifact owned by the blob: [Blob.Close] removes it, and a
// cleanup attached to the blob removes it once the blob becomes unreachable.
// A caller that drops the blob without closing it therefore does not keep the
// file around for the lifetime of the process, which matches the reclamation an
// in-memory blob got for free.
type Blob struct {
	*filesystem.Blob
	file *tempFile
}

var (
	_ blob.ReadOnlyBlob          = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
	_ io.Closer                  = (*Blob)(nil)
)

// tempFile holds the removal state of the temporary file. It is deliberately
// separate from [Blob]: the cleanup argument must not be able to reach the
// object the cleanup is attached to, which would keep that object alive forever.
type tempFile struct {
	path string
	once sync.Once
}

// remove deletes the file, at most once. A file that is already gone is not an error.
func (f *tempFile) remove() error {
	var err error
	f.once.Do(func() {
		if rmErr := os.Remove(f.path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = rmErr
		}
	})
	return err
}

// newBlob wraps the temporary file at path into a [Blob] owning that file.
func newBlob(path string) (*Blob, error) {
	inner, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, err
	}

	b := &Blob{Blob: inner, file: &tempFile{path: path}}
	runtime.AddCleanup(b, func(f *tempFile) {
		if err := f.remove(); err != nil {
			slog.Warn("failed to remove abandoned temporary download file", "path", f.path, "err", err)
		}
	}, b.file)

	return b, nil
}

// Close removes the temporary file backing the blob. It is safe to call multiple
// times and from multiple goroutines. Once closed, the blob can no longer be read.
func (b *Blob) Close() error {
	if err := b.file.remove(); err != nil {
		return fmt.Errorf("failed to remove temporary download file %q: %w", b.file.path, err)
	}
	return nil
}

// ReadCloser returns a reader over the temporary file. The reader keeps the blob
// alive until it is closed, so the cleanup cannot remove the file mid-read.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.Blob.ReadCloser()
	if err != nil {
		return nil, err
	}
	return &retainingReadCloser{ReadCloser: rc, blob: b}, nil
}

// retainingReadCloser keeps a reference to the blob it reads from so that the
// blob stays reachable for as long as the reader is open.
type retainingReadCloser struct {
	io.ReadCloser
	blob *Blob
}

func (r *retainingReadCloser) Close() error {
	err := r.ReadCloser.Close()
	runtime.KeepAlive(r.blob)
	return err
}
