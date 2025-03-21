package ctf

import (
	"io"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// CASFileBlob represents a content-addressable storage (CAS) file blob.
type CASFileBlob struct {
	blob *filesystem.Blob

	mu     sync.RWMutex
	digest string
}

var (
	_ blob.ReadOnlyBlob          = (*CASFileBlob)(nil)
	_ blob.DigestAware           = (*CASFileBlob)(nil)
	_ blob.DigestPrecalculatable = (*CASFileBlob)(nil)
	_ blob.SizeAware             = (*CASFileBlob)(nil)
)

// NewCASFileBlob creates a new CASFileBlob instance.
func NewCASFileBlob(fs filesystem.FileSystem, path string) *CASFileBlob {
	return &CASFileBlob{blob: filesystem.NewFileBlob(fs, path)}
}

// ReadCloser returns an io.ReadCloser for the blob.
func (b *CASFileBlob) ReadCloser() (io.ReadCloser, error) {
	return b.blob.ReadCloser()
}

// Digest returns the digest of the blob.
func (b *CASFileBlob) Digest() (digest string, known bool) {
	b.mu.RLock()
	if b.digest != "" {
		defer b.mu.RUnlock()
		d := b.digest
		return d, true
	}
	b.mu.RUnlock()

	b.mu.Lock()
	dig, known := b.blob.Digest()
	if !known {
		defer b.mu.Unlock()
		return "", false
	}
	defer b.mu.Unlock()
	b.digest = dig
	return dig, true
}

// HasPrecalculatedDigest checks if a digest is already stored.
func (b *CASFileBlob) HasPrecalculatedDigest() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.digest != ""
}

// SetPrecalculatedDigest sets the digest, ensuring thread safety.
func (b *CASFileBlob) SetPrecalculatedDigest(digest string) {
	if digest == "" {
		return // Avoid overwriting with an empty digest
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.digest = digest
}

// Size returns the size of the blob.
func (b *CASFileBlob) Size() int64 {
	return b.blob.Size()
}
