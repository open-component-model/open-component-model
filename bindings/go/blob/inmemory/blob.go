package inmemory

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/opencontainers/go-digest"
)

// New forwards a given [io.Reader] to be able to be used as a ReadOnlyBlob.
// It does this by wrapping the [io.Reader] in a Blob, to allow for caching information such as
// the digest and size of the blob, as well as independent repeated access to the blob data.
// Note that this should only be used without MemoryBlobOption if no sizing and digest information is present.
// Otherwise, use WithSize, WithDigest, or WithMediaType to set the size, digest, and media type of the blob in advance.
func New(r io.Reader, opts ...MemoryBlobOption) *Blob {
	b := newMemoryBlobFromUnknownSource(r)
	for _, opt := range opts {
		opt.ApplyToMemoryBlob(b)
	}
	return b
}

// Blob is a read-only blob that reads from an [io.Reader] once via Load and stores the data in memory.
type Blob struct {
	mu   sync.RWMutex
	data []byte // the data read from source during Blob.Load

	size      int64         // size of the blob, if loaded or set in advance
	digest    digest.Digest // digest of the blob, if loaded or set in advance
	mediaType string        // media type of the blob, if set in advance

	source io.Reader // source is loaded via Blob.Load on the first ReadCloser call.
	loaded bool      // indicates if the data has been loaded from source.
	err    error     // error encountered during Blob.Load, if any
}

// ReadCloser returns a reader to incrementally access byte stream content
// It is the caller's responsibility to close the reader.
//
// ReadCloser MUST be safe for concurrent use, serializing access as necessary.
// ReadCloser MUST be able to be called multiple times, with each invocation
// returning a new reader, that starts from the beginning of the blob.
//
// Note that this behavior is not parallel to WriteableBlob.WriteCloser
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	if err := b.Load(); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return io.NopCloser(bytes.NewReader(b.data)), nil
}

// Load reads the data from the source [io.Reader] and stores it in memory.
// It also calculates the digest and size of the blob.
// If the data is already loaded, it returns nil without reloading, as a reload is not necessary.
func (b *Blob) Load() (err error) {
	b.mu.RLock()
	if b.loaded {
		b.mu.RUnlock()
		return b.err // already loaded
	}
	b.mu.RUnlock()

	b.mu.Lock()
	defer func() {
		b.loaded = true
		b.err = err // store the error if any occurred because Load should not be called again
		b.mu.Unlock()
	}()

	var data bytes.Buffer

	digester := digest.Canonical.Digester()
	sourceWithDigest := io.TeeReader(b.source, digester.Hash())

	if b.size > 0 {
		// if we have a pre-set size, we can use io.CopyN to limit the read.
		_, err = io.CopyN(&data, sourceWithDigest, b.size)
	} else {
		_, err = io.Copy(&data, sourceWithDigest)
		b.size = int64(data.Len())
	}
	if err != nil {
		return err
	}

	// if we have a pre-set digest, we can use it to verify the loaded data.
	if loaded := digester.Digest(); b.digest == "" {
		b.digest = loaded
	} else if b.digest != loaded {
		return fmt.Errorf("data from pre-set digest %q differed from loaded digest %q", b.digest, loaded)
	}

	b.data = data.Bytes()

	return nil
}

func (b *Blob) Size() int64 {
	if b.Load() != nil {
		return -1
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	// the size is always known based on its buffer
	return b.size
}

func (b *Blob) HasPrecalculatedSize() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size > -1
}

func (b *Blob) SetPrecalculatedSize(size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size = size
}

func (b *Blob) Digest() (string, bool) {
	if b.Load() != nil {
		return "", false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.digest.String(), true
}

func (b *Blob) HasPrecalculatedDigest() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.digest != ""
}

func (b *Blob) SetPrecalculatedDigest(dig string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.digest = digest.Digest(dig)
}

func (b *Blob) MediaType() (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.mediaType, true
}

func (b *Blob) SetMediaType(mediaType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mediaType = mediaType
}

func newMemoryBlobFromUnknownSource(source io.Reader) *Blob {
	return &Blob{
		source:    source,
		mediaType: "application/octet-stream",
		size:      -1,
	}
}
