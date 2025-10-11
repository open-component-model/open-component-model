package blob

import (
	"io"
)

// Blob is an interface that represents a Binary Large Object.
// It's main purpose is to provide an abstraction over purpose to be able to only
// interact with content on it's data/byte level.
// A Blob can be interacted with both for reading and writing.
type Blob interface {
	ReadOnlyBlob
	WriteableBlob
}

// WriteableBlob is an interface that represents a Binary Large Object that can be written to.
type WriteableBlob interface {
	// WriteCloser returns a writer to incrementally write byte stream content
	// It is the caller's responsibility to close the writer.
	//
	// WriteCloser MUST be safe for concurrent use, serializing access as necessary.
	// WriteCloser MUST be able to be called multiple times, which each invocation
	// returning a new writer, that appends to the previous one.
	WriteCloser() (io.WriteCloser, error)
}

type ReadOnlyBlob interface {
	// ReadCloser returns a reader to incrementally access byte stream content
	// It is the caller's responsibility to close the reader.
	//
	// ReadCloser MUST be safe for concurrent use, serializing access as necessary.
	// ReadCloser MUST be able to be called multiple times, where each invocation
	// returning a new reader, that starts from the beginning of the blob.
	//
	// Note that this behavior is not parallel to WriteableBlob.WriteCloser
	ReadCloser() (io.ReadCloser, error)
}

// SizeUnknown is a constant that represents an unknown size of a blob.
const SizeUnknown int64 = -1

// SizeAware is an interface that represents any arbitrary object that can be sized.
//
// Size is used to always determine the size of the object in bytes.
type SizeAware interface {
	// Size returns the blob size in bytes if known.
	// If the size is unknown, it MUST return SizeUnknown.
	Size() (size int64)
}

// SizePrecalculatable is an interface that represents any arbitrary object that can be set with Sizes
// ahead of a potential read/write operation.
//
// HasPrecalculatedSize is used to determine if the size of the object is known in advance.
// SetPrecalculatedSize is used to set the size of the object if it is known in advance.
type SizePrecalculatable interface {
	// HasPrecalculatedSize returns true if a precalculated size of the blob is known in advance.
	HasPrecalculatedSize() bool
	// SetPrecalculatedSize sets the size of the blob if it is known in advance.
	// It MUST be safe for concurrent use, serializing as necessary.
	// It MUST be safe to call multiple times, with the same or greater different size.
	// Once called, HasPrecalculatedSize MUST return true.
	// If called with a size less than the current size, it MUST ignore this size.
	SetPrecalculatedSize(size int64)
}

// DigestAware is an interface that represents any arbitrary object that can be digested.
//
// Digest is used to always determine the digest of the object.
type DigestAware interface {
	// Digest returns the blob digest if known.
	Digest() (digest string, known bool)
}

// DigestPrecalculatable is an interface that represents any arbitrary object that can be set with Digests
// ahead of a potential read/write operation.
//
// HasPrecalculatedDigest is used to determine if the digest of the object is known in advance.
// SetPrecalculatedDigest is used to set the digest of the object if it is known in advance.
type DigestPrecalculatable interface {
	// HasPrecalculatedDigest returns true if a precalculated digest of the blob is known in advance.
	HasPrecalculatedDigest() bool
	// SetPrecalculatedDigest sets the digest of the blob if it is known in advance.
	// Once set, HasPrecalculatedDigest must return true.
	// It MUST be safe for concurrent use, serializing as necessary.
	// It MUST be safe to call multiple times, overwriting the digest every time.
	SetPrecalculatedDigest(digest string)
}

// MediaTypeAware is an interface that represents any arbitrary object that can be interpreted as an object that is associated with a MediaType.
//
// Even though Media Types are not a part of the core specification, they are used in many places for content-type awareness.
// SetMediaType is used to set the media type of the object if it is known.
type MediaTypeAware interface {
	// MediaType returns the media type of the blob if known.
	MediaType() (mediaType string, known bool)
}

// MediaTypeOverrideable is an interface that represents any arbitrary object that can be overwritten with custom MediaTypes
// ahead of a potential read/write operation.
type MediaTypeOverrideable interface {
	// SetMediaType overrides the media type of the blob if it is known in advance.
	// It MUST be safe for concurrent use, serializing as necessary.
	// It MUST be safe to call multiple times, overwriting the mediaType every time.
	SetMediaType(mediaType string)
}
