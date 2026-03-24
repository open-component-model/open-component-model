package deployer

import (
	"errors"
	"fmt"
	"io"

	"ocm.software/open-component-model/bindings/go/blob"
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

// getLimitedReader opens a reader from resourceBlob and enforces maxResourceSizeMiB.
// If the limit is 0 it is a no-op and the reader is returned unwrapped.
// The opportunistic pre-check uses blob.SizeAware to reject without reading any data.
// A limitedReadCloser safety-net is always wrapped around the reader when a limit is set.
func getLimitedReader(resourceBlob blob.ReadOnlyBlob, maxResourceSizeMiB int64) (io.ReadCloser, error) {
	reader, err := resourceBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("getting reader for resource blob: %w", err)
	}

	if maxResourceSizeMiB == 0 {
		return reader, nil
	}

	maxBytes := maxResourceSizeMiB * 1024 * 1024

	// Opportunistic pre-check: if the blob declares its size and it already exceeds
	// the limit, reject without reading a single byte.
	if sizeAware, ok := resourceBlob.(blob.SizeAware); ok {
		if size := sizeAware.Size(); size != blob.SizeUnknown && size > maxBytes {
			return nil, fmt.Errorf("resource size %d bytes exceeds maximum allowed size of %d MiB", size, maxResourceSizeMiB)
		}
	}

	// Safety net: wrap the reader so that reading beyond the limit returns an error.
	return &limitedReadCloser{Closer: reader, limited: &io.LimitedReader{R: reader, N: maxBytes}}, nil
}
