package inmemory

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/opencontainers/go-digest"
)

// New forwards a given [io.Reader] to be able to be used as a ReadOnlyBlob.
// It does this by wrapping the [io.Reader] in a Blob, to allow for caching information such as
// the digest and size of the blob, as well as independent repeated access to the blob data.
// Note that this should only be used without MemoryBlobOption if no sizing and digest information is present.
// Otherwise, use WithSize, WithDigest, or WithMediaType to set the size, digest, and media type of the blob in advance.
func New(r io.Reader, opts ...MemoryBlobOption) *Blob {
	b := newMemoryBlobFromUnknownSource()
	for _, opt := range opts {
		opt.ApplyToMemoryBlob(b)
	}

	b.load = sync.OnceValue(func() error {
		return storeSourceInBlob(b, r)
	})

	return b
}

// Blob is a read-only blob that reads from an [io.Reader] once via Load and stores the data in memory.
type Blob struct {
	data []byte // the data read from source during Blob.Load

	size      atomic.Int64                  // size of the blob, if loaded or set in advance
	digest    atomic.Pointer[digest.Digest] // digest of the blob, if loaded or set in advance
	mediaType atomic.Pointer[string]        // media type of the blob, if set in advance

	load func() error
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

	return io.NopCloser(bytes.NewReader(b.data)), nil
}

// Load reads the data from the source [io.Reader] and stores it in memory (at most once).
// It also calculates the digest and size of the blob.
// If the data is already loaded, it will return the result of the last load.
func (b *Blob) Load() (err error) {
	return b.load()
}

func (b *Blob) Data() []byte {
	if err := b.Load(); err != nil {
		return nil
	}

	return b.data
}

func (b *Blob) Size() int64 {
	if b.Load() != nil {
		return -1
	}

	// the size is always known based on its buffer
	return b.size.Load()
}

func (b *Blob) HasPrecalculatedSize() bool {
	return b.size.Load() > -1
}

func (b *Blob) SetPrecalculatedSize(size int64) {
	b.size.Store(size)
}

func (b *Blob) Digest() (string, bool) {
	if b.Load() != nil {
		return "", false
	}

	return b.digest.Load().String(), true
}

func (b *Blob) HasPrecalculatedDigest() bool {
	return b.digest.Load().String() != ""
}

func (b *Blob) SetPrecalculatedDigest(dig string) {
	d := digest.Digest(dig)
	b.digest.Store(&d)
}

func (b *Blob) MediaType() (string, bool) {
	return *b.mediaType.Load(), true
}

func (b *Blob) SetMediaType(mediaType string) {
	b.mediaType.Store(&mediaType)
}

func newMemoryBlobFromUnknownSource() *Blob {
	b := &Blob{}
	mt := "application/octet-stream"
	b.mediaType.Store(&mt)
	b.size.Store(-1)
	d := digest.Digest("")
	b.digest.Store(&d)
	return b
}

func storeSourceInBlob(b *Blob, source io.Reader) error {
	// Either compute a new digest or verify against an existing one.
	var reader io.Reader
	var digester digest.Digester
	var verifier digest.Verifier
	if loadedDigest := b.digest.Load(); loadedDigest.String() == "" {
		digester = digest.Canonical.Digester()
		reader = io.TeeReader(source, digester.Hash())
	} else {
		if err := loadedDigest.Validate(); err != nil {
			return fmt.Errorf("invalid digest %q: %w", loadedDigest, err)
		}
		verifier = loadedDigest.Verifier()
		reader = io.TeeReader(source, verifier)
	}

	// either read the data into a pre-set size or read all data
	// if the size is set, we can use io.ReadFull to limit the read.
	// if the size is not set, we can use io.ReadAll to read all data with buffering
	var err error
	if size := b.size.Load(); size > 0 {
		b.data = make([]byte, size)
		// if we have a pre-set size, we can use io.CopyN to limit the read.
		_, err = io.ReadFull(reader, b.data)
	} else {
		b.data, err = io.ReadAll(reader)
		b.size.Store(int64(len(b.data)))
	}
	if err != nil {
		return err
	}

	// if we have a digest we just computed, store it
	// if we have a verifier, we need to check if the data matches the loaded digest
	switch {
	case digester != nil:
		newDigest := digester.Digest()
		b.digest.Store(&newDigest)
	case verifier != nil:
		if !verifier.Verified() {
			return fmt.Errorf("differed from loaded digest")
		}
	}

	return nil
}
