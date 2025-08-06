// Package inmemory provides an in-memory caching implementation for blob storage.
// It wraps a ReadOnlyBlob and caches its data in memory for efficient repeated access.
package inmemory

import (
	"bytes"
	"errors"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Cache creates a new in-memory cached blob from a ReadOnlyBlob.
// The returned blob will cache the data of the original blob in memory when this function is called.
// If the caching fails, it returns an error.
func Cache(b blob.ReadOnlyBlob) (_ *Blob, err error) {
	// Get a reader from the source
	readCloser, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, readCloser.Close())
	}()

	// Get size first
	size := blob.SizeUnknown
	if sizeAware, ok := b.(blob.SizeAware); ok {
		size = sizeAware.Size()
	}

	digester := digest.Canonical.Digester()
	reader := io.TeeReader(readCloser, digester.Hash())

	var data []byte
	if size == blob.SizeUnknown {
		// If size is unknown, fall back to ReadAll
		if data, err = io.ReadAll(reader); err != nil {
			return nil, err
		}
	} else {
		// Create buffer with exact size
		data = make([]byte, size)
		if _, err = io.ReadFull(reader, data); err != nil {
			return nil, err
		}
	}

	var mediaType string
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		if mediaTypeFromBlob, ok := mediaTypeAware.MediaType(); ok {
			mediaType = mediaTypeFromBlob
		}
	}

	return &Blob{mediaType: mediaType, data: data, digest: digester.Digest()}, nil
}

// Blob implements an in-memory caching wrapper around a ReadOnlyBlob.
// It stores the blob's data in memory after the first read for efficient
// subsequent access. The implementation is thread-safe.
type Blob struct {
	mediaType string
	data      []byte
	digest    digest.Digest
}

// ReadCloser returns an io.ReadCloser that provides access to the blob's data.
// The data is cached in memory after the first read for efficient subsequent access.
func (c *Blob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(c.data)), nil
}

// Size returns the size of the blob in bytes.
// Can never return SizeUnknown, as the size is always known after caching.
func (c *Blob) Size() int64 {
	return int64(len(c.data))
}

// Digest calculates and returns the digest of the blob's data.
// Can always return the digest as it is calculated during caching.
func (c *Blob) Digest() (string, bool) {
	return c.digest.String(), true
}

// MediaType returns the media type of the blob if it is available.
// If the underlying blob implements MediaTypeAware, its media type is returned.
// Otherwise, returns an empty string and false.
func (c *Blob) MediaType() (mediaType string, known bool) {
	return c.mediaType, c.mediaType != ""
}

// Data returns the complete data of the blob as a byte slice.
// The data is cached in memory after the first read for efficient subsequent access.
func (c *Blob) Data() []byte {
	// Try to get cached data first
	return c.data
}
