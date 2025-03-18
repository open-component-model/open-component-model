package ctf

import (
	"io"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// CASFileBlob is a wrapper around a filesystem.Blob that adds a digest field.
// This is to implement the concept of a Content Addressable Storage (CAS), in which the data
// is stored in a way that the content itself is used as the address. (the digest).
// Because Content is not allowed to change without changing the digest, this only exposes the
// blob.ReadOnlyBlob interface.
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

func NewCASFileBlob(fs filesystem.FileSystem, path string) *CASFileBlob {
	return &CASFileBlob{
		blob: filesystem.NewFileBlob(fs, path),
	}
}

func (b *CASFileBlob) ReadCloser() (io.ReadCloser, error) {
	return b.blob.ReadCloser()
}

func (b *CASFileBlob) Digest() (digest string, known bool) {
	if b.HasPrecalculatedDigest() {
		return b.digest, true
	}
	dig, known := b.blob.Digest()
	if !known {
		return "", false
	}
	b.SetPrecalculatedDigest(dig)
	return dig, true
}

func (b *CASFileBlob) HasPrecalculatedDigest() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.digest != ""
}

func (b *CASFileBlob) SetPrecalculatedDigest(digest string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.digest = digest
}

func (b *CASFileBlob) Size() (size int64) {
	return b.blob.Size()
}
