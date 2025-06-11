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
	buf       []byte
	digest    string
	size      int64
	loaded    bool
	reader    io.Reader
	mediaType string
	cursor    int64
}

var (
	// By buffering the data, we can calculate the size and digest of the data
	// from the buffer!
	_ SizeAware   = &EagerBufferedReader{}
	_ DigestAware = &EagerBufferedReader{}

	// We can also set the media type of the buffer if needed
	_ MediaTypeAware = &EagerBufferedReader{}

	_ io.ReadSeeker = &EagerBufferedReader{}
)

func (b *EagerBufferedReader) LoadEagerly() error {
	if b.Loaded() {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	var buf bytes.Buffer
	if n, err := io.Copy(&buf, b.reader); err != nil {
		return err
	} else {
		b.size = n
		b.buf = buf.Bytes()
	}

	// Calculate digest from the buffer
	dig, err := digest.FromReader(bytes.NewReader(b.buf))
	if err != nil {
		return err
	}
	b.digest = dig.String()
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
		// return 0 and not SizeUnknown because readers that return negative size
		// can cause panic in read implementations in stdlib.
		return 0, err
	}
	reader := bytes.NewReader(b.buf)
	n, err = reader.ReadAt(p, b.cursor)
	if err != nil {
		return n, err
	}
	b.cursor += int64(n)
	if b.cursor >= int64(len(b.buf)) {
		_, err = b.Seek(0, io.SeekStart) // auto-reset cursor if we are at the end of the buffer
	}
	return n, err
}

func (b *EagerBufferedReader) Seek(offset int64, whence int) (int64, error) {
	if err := b.LoadEagerly(); err != nil {
		return 0, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	switch whence {
	default:
		// default to start if whence is invalid
		fallthrough
	case io.SeekStart:
		if offset > int64(len(b.buf)) {
			return 0, io.ErrUnexpectedEOF
		} else {
			b.cursor = offset
		}
	case io.SeekCurrent:
		b.cursor += offset
	case io.SeekEnd:
		b.cursor = int64(len(b.buf)) + offset
	}

	if b.cursor < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	return b.cursor, nil
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

func (b *EagerBufferedReader) Size() int64 {
	if b.LoadEagerly() != nil {
		return SizeUnknown
	}
	// the size is always known based on its buffer
	return b.size
}

func (b *EagerBufferedReader) HasPrecalculatedSize() bool {
	// we know the size if we have loaded the data already
	return b.Loaded()
}

func (b *EagerBufferedReader) SetPrecalculatedSize(size int64) {
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
