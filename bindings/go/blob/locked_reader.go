package blob

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

type LockedReader struct {
	pr *io.PipeReader
}

func NewLockedReader(ctx context.Context, mu *sync.RWMutex, blob ReadOnlyBlob) (io.ReadCloser, error) {
	// Create pipe for streaming

	var rc io.ReadCloser
	var err error
	if rc, err = blob.ReadCloser(); err != nil {
		return nil, fmt.Errorf("unable to get reader: %w", err)
	}
	pr, pw := io.Pipe()
	go func() {
		mu.RLock()

		defer func() {
			if err := rc.Close(); err != nil {
				slog.ErrorContext(ctx, "unable to close reader", slog.String("error", err.Error()))
			}
			mu.RUnlock()
		}()

		// Copy data through pipe
		if _, err := io.Copy(pw, rc); err != nil {
			if err := pw.CloseWithError(fmt.Errorf("unable to copy data: %w", err)); err != nil {
				slog.ErrorContext(ctx, "unable to close pipe with error", slog.String("error", err.Error()))
			}
			return
		}

		if err := pw.Close(); err != nil {
			slog.ErrorContext(ctx, "unable to close reader", slog.String("error", err.Error()))
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
