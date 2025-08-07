package direct

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
)

// NewFromBuffer creates a Blob from a bytes.Buffer. If unsafe is false,
// the buffer contents are cloned when this method is called to avoid relying on the underlying buffer not being mutated.
// However, this is cloning the data in-memory, while allowing reuse of the buffers underlying slice safely.
// Additional DirectBlobOption values can be provided to configure the Blob.
//
// See New for details on how the Blob is constructed.
func NewFromBuffer(buffer *bytes.Buffer, unsafe bool, opts ...DirectBlobOption) *Blob {
	view := buffer.Bytes()
	if !unsafe {
		// Clone the slice to prevent modifications to the original buffer
		view = bytes.Clone(view)
	}
	return NewFromBytes(view,
		append([]DirectBlobOption{WithSize(int64(len(view)))}, opts...)...)
}

// NewFromBytes creates a Blob from a byte slice. It wraps the slice in a reader
// and applies a WithSize option based on its length. Further DirectBlobOption
// values can customize behavior.
//
// See New for more details on how the Blob is constructed.
func NewFromBytes(data []byte, opts ...DirectBlobOption) *Blob {
	return New(bytes.NewReader(data),
		append([]DirectBlobOption{WithSize(int64(len(data)))}, opts...)...)
}

// New constructs a Blob from a [io.ReaderAt] source, applying any DirectBlobOption values.
// By default, the media type is set to "application/octet-stream" if not overwritten by
// WithMediaType.
//
// New attempts compute the size by
//   - using a pre-known size if available via WithSize option
//   - calling Size() int64 if available
//   - seeking to the end of the reader and rewinding back to the start via io.Seeker.
//   - If it cannot seek, size defaults to -1 (SizeUnknown).
//
// Note that if you do not have the ability to open readers on demand on the source,
// you can use a memory buffered blob instead (see package inmemory for that) to avoid the need for seeking or
// to allow repetitive reads.
//
// If possible, you should also use native implementations that access the underlying data structures directly.
// For example, for filesystem blobs, use the filesystem package which directly works with stat calls.
//
// See Blob for more details on how the underlying implementation behaves.
func New(src io.ReaderAt, opts ...DirectBlobOption) *Blob {
	b := &Blob{
		reader:    src,
		mediaType: atomic.Pointer[string]{},
	}

	mediaType := "application/octet-stream"
	b.mediaType.Store(&mediaType)

	// Apply each option to configure the Blob
	for _, o := range opts {
		o.ApplyToDirectBlob(b)
	}
	if b.size == nil {
		b.size = wellKnownBlobSize(src)
	}

	return b
}

// Blob represents immutable binary data that can be read independently with each call to ReadCloser.
// It is designed to be thread-safe and allows concurrent access to its methods.
// Thus it can be considered a "direct" blob implementation that does not rely on in-memory buffering,
// but rather on the underlying [io.ReaderAt] source for reading data.
//
// It supports lazy evaluation of size. When constructed via
// New, by default size does not use up buffer space, but can still be determined when the reader supports io.Seeker.
type Blob struct {
	// base for creating io.SectionReader instances
	reader io.ReaderAt
	// MIME type of the data
	mediaType atomic.Pointer[string]
	// size is a lazily-evaluated function returning the size of the underlying returned readers.
	size func() (int64, error)
}

// Size returns the length of the blob in bytes, or -1 (SizeUnknown) if an error occurs.
func (b *Blob) Size() int64 {
	sz, err := b.size()
	if err != nil {
		return -1
	}
	return sz
}

// ReadCloser returns a new io.ReadCloser that streams from the start
// of the blob to its size. Closing it does not close the underlying source.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	size, err := b.size()
	if err != nil {
		return nil, err
	}
	// NewSectionReader reads from offset 0 up to the blob size
	reader := io.NewSectionReader(b.reader, 0, size)
	return io.NopCloser(reader), nil
}

// SetMediaType updates the MIME type of the blob.
func (b *Blob) SetMediaType(s string) {
	b.mediaType.Store(&s)
}

// MediaType returns the blob's MIME type and a boolean flag indicating success,
// as we always at least have a default mime type of "application/octet-stream".
func (b *Blob) MediaType() (string, bool) {
	return *b.mediaType.Load(), true
}

// wellKnownBlobSize returns a function that computes the size of the blob using
// some well known attributes that may be available on a given source.
func wellKnownBlobSize(src any) func() (int64, error) {
	// if we have size information available on the source via interface, we can use it directly.
	// most libraries expose this interface (including our own blob and go stdlib), so it is useful to shortcut to.
	type hasSize64 interface{ Size() int64 }
	if sizeAware, ok := src.(hasSize64); ok {
		return func() (int64, error) {
			return sizeAware.Size(), nil
		}
	}

	// If no explicit size was set, we can still attempt to compute and cache it lazily via seek
	// io.Seeker can be used to atomically compute size. A typical example of this is a plain file descriptor.
	if seeker, ok := src.(io.Seeker); ok {
		return sync.OnceValues(func() (int64, error) {
			// Move to end to get total length
			size, err := seeker.Seek(0, io.SeekEnd)
			if err != nil {
				return -1, err
			}
			// Rewind to beginning
			_, err = seeker.Seek(0, io.SeekStart)
			return size, err
		})
	}

	// if we have no other way of determining size, we default to -1 (SizeUnknown)
	return func() (int64, error) {
		return -1, nil
	}
}
