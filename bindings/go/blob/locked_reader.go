package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// LockedReader provides thread-safe access to blob data by wrapping reads
// with mutex synchronization. It uses a pipe to stream data while holding
// a read lock on the underlying blob.
type LockedReader struct {
	pr *io.PipeReader
}

// NewLockedReader creates a thread-safe reader for a blob that acquires a read lock
// before streaming data. The lock is held during the entire copy operation to ensure
// consistent access and prevent concurrent writes.
func NewLockedReader(ctx context.Context, mu *sync.RWMutex, blob ReadOnlyBlob) io.ReadCloser {
	pr, pw := io.Pipe()
	// Copy goroutine - performs the actual data copy
	go func() {
		mu.RLock()
		defer mu.RUnlock()
		done := make(chan struct{})
		var copyErrs error
		var err error
		var rc io.ReadCloser
		closePipe := func() {
			if copyErrs != nil {
				if err := pw.CloseWithError(fmt.Errorf("unable to copy data: %w", copyErrs)); err != nil {
					slog.ErrorContext(ctx, "unable to close pipe with error", slog.String("error", err.Error()))
				}
			} else {
				if err := pw.Close(); err != nil {
					slog.ErrorContext(ctx, "failed to close pipe", slog.String("error", err.Error()))
				}
			}
			if err := rc.Close(); err != nil {
				slog.ErrorContext(ctx, "failed to close reader", slog.String("error", err.Error()))
			}
		}
		// Get reader
		if rc, err = blob.ReadCloser(); err != nil {
			if err := pw.CloseWithError(fmt.Errorf("unable to get reader: %w", err)); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe with error", slog.String("error", err.Error()))
			}
			return
		}

		go func() {
			defer close(done)
			if _, err = io.Copy(pw, rc); err != nil {
				copyErrs = errors.Join(copyErrs, err)
			}
		}()

		select {
		case <-ctx.Done():
			copyErrs = errors.Join(copyErrs, ctx.Err())
			closePipe()
			<-done // Wait for copy to finish
		case <-done:
			closePipe()
		}
	}()

	return &LockedReader{pr: pr}
}

func (lr *LockedReader) Read(p []byte) (n int, err error) {
	return lr.pr.Read(p)
}

func (lr *LockedReader) Close() error {
	return lr.pr.Close()
}
