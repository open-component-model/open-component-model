package blob

import (
	"context"
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
func NewLockedReader(ctx context.Context, mu *sync.RWMutex, blob ReadOnlyBlob) (io.ReadCloser, error) {
	mu.RLock()

	// Create pipe for streaming
	pr, pw := io.Pipe()

	var rc io.ReadCloser

	// Context cancellation goroutine - monitors for cancellation and interrupts pipe
	go func() {
		<-ctx.Done()
		if err := ctx.Err(); err != nil {
			if err := pw.CloseWithError(err); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe with cancellation error", slog.String("error", err.Error()))
			}
			return
		}
	}()

	// Copy goroutine - performs the actual data copy
	go func() {
		var err error

		// Ensure lock is released
		defer mu.RUnlock()
		// Ensure pipe is closed
		defer func() {
			if err := pw.Close(); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe writer", slog.String("error", err.Error()))
			}
		}()
		// Get reader
		if rc, err = blob.ReadCloser(); err != nil {
			if err := pw.CloseWithError(fmt.Errorf("unable to get reader: %w", err)); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe with error", slog.String("error", err.Error()))
			}
			return
		}

		// Ensure reader is closed once it is available
		defer func() {
			if err := rc.Close(); err != nil {
				slog.ErrorContext(ctx, "unable to close reader", slog.String("error", err.Error()))
			}
		}()

		// Copy data through pipe
		if _, err := io.Copy(pw, rc); err != nil {
			if err := pw.CloseWithError(fmt.Errorf("unable to copy data: %w", err)); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe with error", slog.String("error", err.Error()))
			}
			return
		}

	}()

	return &LockedReader{pr: pr}, nil
}

func (lr *LockedReader) Read(p []byte) (n int, err error) {
	return lr.pr.Read(p)
}

func (lr *LockedReader) Close() error {
	return lr.pr.Close()
}
