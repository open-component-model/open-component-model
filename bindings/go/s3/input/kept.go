package input

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
)

// keptBlob serves its first read from the file the object was downloaded to when the input
// was processed, and removes the file when that reader is closed, so the object is downloaded
// once. Later reads are served by fetch. If nothing reads the blob, the file is removed when
// the context ends.
type keptBlob struct {
	fetch blob.ReadOnlyBlob

	mu   sync.Mutex
	path string
	stop func() bool
}

func newKeptBlob(ctx context.Context, path string, fetch blob.ReadOnlyBlob) *keptBlob {
	b := &keptBlob{fetch: fetch, path: path}
	// The lock keeps an already ended context from taking the file before stop is set.
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stop = context.AfterFunc(ctx, func() {
		if p := b.take(); p != "" {
			removeFile(context.WithoutCancel(ctx), p)
		}
	})
	return b
}

func (b *keptBlob) ReadCloser() (io.ReadCloser, error) {
	path := b.take()
	if path == "" {
		return b.fetch.ReadCloser()
	}
	f, err := os.Open(path)
	if err != nil {
		removeFile(context.Background(), path)
		return nil, err
	}
	return &removingFile{File: f}, nil
}

// take hands the file to its only user: the first read or the context ending.
func (b *keptBlob) take() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	path := b.path
	b.path = ""
	if path != "" && b.stop != nil {
		b.stop()
	}
	return path
}

// removingFile removes the file once it is closed.
type removingFile struct{ *os.File }

func (f *removingFile) Close() error {
	return errors.Join(f.File.Close(), os.Remove(f.Name()))
}

func removeFile(ctx context.Context, path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.WarnContext(ctx, "failed to remove downloaded s3 input", "file", path, "err", err)
	}
}
