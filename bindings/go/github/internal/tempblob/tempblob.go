// Package tempblob buffers a byte stream into a temporary file and exposes it
// as a blob.ReadOnlyBlob, so large remote blobs are held on the filesystem
// instead of in memory.
//
// Cleanup does not rely on the consumer: no DownloadResource caller closes the
// blob it receives, so a blob that is never closed still removes its file once
// it becomes unreachable. Close reclaims the file immediately for callers that
// know when they are done.
package tempblob

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"sync/atomic"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Blob is a blob.ReadOnlyBlob backed by a temporary file.
type Blob struct {
	path      string
	size      int64
	mediaType string
	closed    atomic.Bool
	// cleanup removes the file if Close never runs. A runtime.Cleanup holds no
	// pointer to its object, so keeping it here does not pin the Blob.
	cleanup runtime.Cleanup
}

var (
	_ blob.ReadOnlyBlob   = (*Blob)(nil)
	_ blob.SizeAware      = (*Blob)(nil)
	_ blob.MediaTypeAware = (*Blob)(nil)
	_ io.Closer           = (*Blob)(nil)
)

// New streams r into a temporary file created under dir with the given name
// pattern (as accepted by os.CreateTemp) and returns it as a blob. An empty
// dir uses the operating system's temporary directory.
//
// The file is removed when the returned Blob is closed, or, failing that, once
// it becomes unreachable.
func New(dir, pattern string, r io.Reader, mediaType string) (_ *Blob, err error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("error creating temporary directory %q: %w", dir, err)
		}
	}

	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for blob: %w", err)
	}
	path := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	size, err := io.Copy(file, r)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("error buffering blob into %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("error closing temporary file %q: %w", path, err)
	}

	b := &Blob{path: path, size: size, mediaType: mediaType}

	// The func must not capture b, or b would never become unreachable and the
	// cleanup would never run.
	b.cleanup = runtime.AddCleanup(b, func(path string) { _ = os.Remove(path) }, path)

	return b, nil
}

// ReadCloser returns a reader over the buffered file. It may be called any
// number of times; each call returns a reader positioned at the start. It
// fails once the blob has been closed.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("blob %q is closed", b.path)
	}
	file, err := os.Open(b.path)
	if err != nil {
		return nil, fmt.Errorf("error opening buffered blob %q: %w", b.path, err)
	}
	return file, nil
}

// Size returns the number of bytes buffered.
func (b *Blob) Size() int64 { return b.size }

// MediaType returns the media type the blob was created with.
func (b *Blob) MediaType() (string, bool) { return b.mediaType, b.mediaType != "" }

// Close removes the buffered file and is idempotent. It is safe to call while
// readers from ReadCloser are still open: on POSIX those readers keep working
// until closed themselves.
//
// If the file cannot be removed, Close reports the error and leaves the blob
// open, so ReadCloser keeps working and Close can be retried; the blob is
// closed only once its file is gone.
func (b *Blob) Close() error {
	if b.closed.Load() {
		return nil
	}
	if err := os.Remove(b.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Leave the cleanup armed so an unreachable blob still reclaims the file.
		return fmt.Errorf("error removing buffered blob %q: %w", b.path, err)
	}
	b.closed.Store(true)
	// File gone: cancel the safety net so it cannot later remove a name that
	// os.CreateTemp may by then have reused.
	b.cleanup.Stop()
	return nil
}
