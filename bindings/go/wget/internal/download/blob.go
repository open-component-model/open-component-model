package download

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"

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
	path string
}

var (
	_ blob.ReadOnlyBlob          = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
	_ io.Closer                  = (*Blob)(nil)
)

// removeTempFile deletes the file at path. A file that is already gone is not an
// error, which makes repeated calls (Close plus the cleanup) idempotent without
// tracking any removal state.
func removeTempFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// newBlob wraps the temporary file at path into a [Blob] owning that file.
func newBlob(path string) (*Blob, error) {
	inner, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, err
	}

	// The cleanup takes the path rather than the blob: an argument that can reach
	// the object the cleanup is attached to would keep it alive forever.
	b := &Blob{Blob: inner, path: path}
	runtime.AddCleanup(b, func(path string) {
		if err := removeTempFile(path); err != nil {
			slog.Warn("failed to remove abandoned temporary download file", "path", path, "err", err)
		}
	}, b.path)

	return b, nil
}

// Close removes the temporary file backing the blob. It is safe to call multiple
// times and from multiple goroutines. Once closed, the blob can no longer be read.
func (b *Blob) Close() error {
	if err := removeTempFile(b.path); err != nil {
		return fmt.Errorf("failed to remove temporary download file %q: %w", b.path, err)
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
