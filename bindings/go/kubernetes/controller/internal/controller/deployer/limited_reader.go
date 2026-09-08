package deployer

import (
	"errors"
	"fmt"
	"io"
)

// limitedReadCloser wraps an io.ReadCloser using io.LimitedReader to cap reads,
// returning an error instead of silent truncation when the limit is exceeded.
type limitedReadCloser struct {
	io.Closer
	limited *io.LimitedReader
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.limited.Read(p)
	if err == nil && l.limited.N == 0 {
		// The LimitedReader is exhausted. This could mean the blob is exactly at the
		// limit (fine) or that there is more data beyond it (overflow). Probe one byte
		// from the underlying reader to distinguish the two cases.
		var probe [1]byte
		_, probeErr := l.limited.R.Read(probe[:])
		switch {
		case errors.Is(probeErr, io.EOF):
			// Blob fits exactly within the limit — return the bytes normally.
			return n, nil
		case probeErr == nil:
			// Probe returned data: the blob exceeds the limit.
			return n, fmt.Errorf("resource exceeds maximum allowed size")
		default:
			// Unexpected I/O error during probe — surface it directly.
			return n, fmt.Errorf("probe failed while checking resource size: %w", probeErr)
		}
	}
	return n, err
}
