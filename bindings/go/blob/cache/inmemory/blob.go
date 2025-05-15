// Package inmemory provides an in-memory caching implementation for blob storage.
// It wraps a ReadOnlyBlob and caches its data in memory for efficient repeated access.
package inmemory

import (
	"bytes"
	"io"
	"sync"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Cache creates a new in-memory cached blob from a ReadOnlyBlob.
// The returned blob will cache the data of the original blob in memory
// for efficient repeated access.
func Cache(b blob.ReadOnlyBlob) *Blob {
	return &Blob{ReadOnlyBlob: b}
}

// Blob implements an in-memory caching wrapper around a ReadOnlyBlob.
// It stores the blob's data in memory after the first read for efficient
// subsequent access. The implementation is thread-safe.
type Blob struct {
	blob.ReadOnlyBlob
	data []byte
	mu   sync.RWMutex
}

// ReadCloser returns an io.ReadCloser that provides access to the blob's data.
// The data is cached in memory after the first read for efficient subsequent access.
func (c *Blob) ReadCloser() (io.ReadCloser, error) {
	// If we have cached data, use it
	if data := c.getData(); data != nil {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	// Get a reader from the source
	reader, err := c.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	// Get size first
	size := c.Size()
	if size == blob.SizeUnknown {
		// If size is unknown, fall back to ReadAll
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		c.setData(data)
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	// Create buffer with exact size
	buf := make([]byte, size)
	if _, err = io.ReadFull(reader, buf); err != nil {
		return nil, err
	}
	c.setData(buf)
	return io.NopCloser(bytes.NewReader(buf)), nil
}

// Size returns the size of the blob in bytes.
// If the size is unknown, it returns blob.SizeUnknown.
func (c *Blob) Size() int64 {
	// If we have cached data, use its length
	if data := c.getData(); data != nil {
		return int64(len(data))
	}

	// Try to get size from the source
	if sizeAware, ok := c.ReadOnlyBlob.(blob.SizeAware); ok {
		return sizeAware.Size()
	}

	// Read and cache the data as a fallback
	// this only gets triggered if we couldn't eagerly lead the size from the source blob
	// and we don't already have data.
	reader, err := c.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return blob.SizeUnknown
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return blob.SizeUnknown
	}
	c.setData(data)
	return int64(len(data))
}

// Digest calculates and returns the digest of the blob's data.
// Returns the digest string and a boolean indicating if the digest was successfully computed.
func (c *Blob) Digest() (string, bool) {
	// If we have cached data, use it
	if data := c.getData(); data != nil {
		dig := digest.FromBytes(data)
		return dig.String(), true
	}

	// For unknown size, we need to read the entire blob
	reader, err := c.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return "", false
	}
	defer reader.Close()

	// Get size first
	size := c.Size()
	hasher := digest.Canonical.Digester()

	if size != blob.SizeUnknown {
		// If we know the size, use a buffer of exact size
		buf := make([]byte, size)
		teeReader := io.TeeReader(reader, hasher.Hash())
		if _, err := io.ReadFull(teeReader, buf); err != nil {
			return "", false
		}
		c.setData(buf)
	} else {
		// For unknown size, use a buffer to store the data
		var buf bytes.Buffer
		teeReader := io.TeeReader(reader, hasher.Hash())
		if _, err := io.Copy(&buf, teeReader); err != nil {
			return "", false
		}
		c.setData(buf.Bytes())
	}

	return hasher.Digest().String(), true
}

// MediaType returns the media type of the blob if it is available.
// If the underlying blob implements MediaTypeAware, its media type is returned.
// Otherwise, returns an empty string and false.
func (c *Blob) MediaType() (mediaType string, known bool) {
	if mediaTypeAware, ok := c.ReadOnlyBlob.(blob.MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

// Data returns the complete data of the blob as a byte slice.
// The data is cached in memory after the first read for efficient subsequent access.
func (c *Blob) Data() ([]byte, error) {
	// Try to get cached data first
	if data := c.getData(); data != nil {
		return data, nil
	}

	// Read and cache the data
	reader, err := c.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	// Get size first
	size := c.Size()
	if size == blob.SizeUnknown {
		// If size is unknown, fall back to ReadAll
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		c.setData(data)
		return data, nil
	}

	// Create buffer with exact size
	buf := make([]byte, size)
	if _, err = io.ReadFull(reader, buf); err != nil {
		return nil, err
	}
	c.setData(buf)
	return buf, nil
}

// setData stores the blob's data in memory.
// This method is thread-safe and should only be called internally.
func (c *Blob) setData(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
}

// getData retrieves the cached data of the blob.
// This method is thread-safe and should only be called internally.
func (c *Blob) getData() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// ClearCache clears the cached data, freeing up memory.
func (c *Blob) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = nil
}
