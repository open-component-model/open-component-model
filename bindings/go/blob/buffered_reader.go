package blob

import (
	"bytes"
	"io"
	"sync"

	"github.com/opencontainers/go-digest"
)

// NewEagerBufferedReader creates a new EagerBufferedReader instance.
func NewEagerBufferedReader(r io.Reader) *EagerBufferedReader {
	return &EagerBufferedReader{
		reader:    r,
		mediaType: "application/octet-stream",
	}
}

// EagerBufferedReader is a reader that can calculate its digest and size
// By eagerly loading the data, and proxying a buffer for it in memory.
// This can lead to extraordinary memory usage for large files, so one should
// be careful when using this without considering the potential size of the data.
type EagerBufferedReader struct {
	mu        sync.RWMutex
	buf       *bytes.Buffer
	digest    string
	size      uint64
	loaded    bool
	reader    io.Reader
	mediaType string
}

var (
	// By buffering the data, we can calculate the size and digest of the data
	// from the buffer!
	_ SizeAware   = &EagerBufferedReader{}
	_ DigestAware = &EagerBufferedReader{}

	// We can also set the media type of the buffer if needed
	_ MediaTypeAware = &EagerBufferedReader{}
)

func (b *EagerBufferedReader) LoadEagerly() error {
	if b.Loaded() {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buf == nil {
		b.buf = &bytes.Buffer{}
	}
	if b.reader != nil {
		b.reader = io.MultiReader(b.buf, b.reader)
	}
	dig, err := digest.FromReader(io.TeeReader(b.reader, b.buf))
	if err != nil {
		return err
	}
	b.digest = dig.String()
	if newSize := uint64(b.buf.Len()); newSize > b.size { //nolint:gosec // G115 can never be negative
		b.size = newSize
	}
	b.loaded = true
	return nil
}

func (b *EagerBufferedReader) Loaded() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loaded
}

func (b *EagerBufferedReader) Read(p []byte) (n int, err error) {
	if err := b.LoadEagerly(); err != nil {
		return -1, err
	}
	return b.buf.Read(p)
}

func (b *EagerBufferedReader) Close() error {
	closeable, ok := b.reader.(io.Closer)
	if !ok {
		return nil
	}
	return closeable.Close()
}

func (b *EagerBufferedReader) Digest() (string, bool) {
	if b.LoadEagerly() != nil {
		return "", false
	}

	// the digest is always known based on its buffer
	return b.digest, true
}

func (b *EagerBufferedReader) HasPrecalculatedDigest() bool {
	return b.Loaded()
}

func (b *EagerBufferedReader) SetPrecalculatedDigest(digest string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.digest = digest
}

func (b *EagerBufferedReader) Size() (uint64, bool) {
	if b.LoadEagerly() != nil {
		return 0, false
	}
	// the size is always known based on its buffer
	return b.size, true
}

func (b *EagerBufferedReader) HasPrecalculatedSize() bool {
	// we know the size if we have loaded the data already
	return b.Loaded()
}

func (b *EagerBufferedReader) SetPrecalculatedSize(size uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size = size
}

func (b *EagerBufferedReader) MediaType() (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.mediaType, true
}

func (b *EagerBufferedReader) SetMediaType(mediaType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mediaType = mediaType
}
