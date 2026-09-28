package download

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"

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
	// headers are the response headers of the download that produced this blob.
	headers http.Header
	// digests holds hex digests computed during the download, keyed by
	// [DigestAlgorithm.Name].
	digests map[string]string
	// precalculated is returned by [Blob.Digest] verbatim when set, avoiding a
	// re-read to recompute the digest.
	precalculated atomic.Pointer[string]
}

var (
	_ blob.ReadOnlyBlob          = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.DigestPrecalculatable = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
	_ io.Closer                  = (*Blob)(nil)
)

// Headers returns the response headers of the producing download.
func (b *Blob) Headers() http.Header {
	return b.headers
}

// Digests returns hex digests computed during the download, keyed by the name
// the caller passed to [WithDigestAlgorithms].
func (b *Blob) Digests() map[string]string {
	return b.digests
}

// Digest returns the precalculated digest when set, otherwise the embedded
// blob's lazily computed digest.
func (b *Blob) Digest() (string, bool) {
	if p := b.precalculated.Load(); p != nil {
		return *p, true
	}
	return b.Blob.Digest()
}

// HasPrecalculatedDigest reports whether a precalculated digest was set.
func (b *Blob) HasPrecalculatedDigest() bool {
	return b.precalculated.Load() != nil
}

// SetPrecalculatedDigest sets the digest returned by [Blob.Digest]. Safe for
// concurrent use.
func (b *Blob) SetPrecalculatedDigest(digest string) {
	b.precalculated.Store(&digest)
}

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
