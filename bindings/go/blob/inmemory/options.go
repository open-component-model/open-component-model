package inmemory

type MemoryBlobOption interface {
	ApplyToMemoryBlob(*Blob)
}

// WithMediaType is a MemoryBlobOption that sets the media type of the Blob.
// When applied, it will set the media type of the Blob to the string value of the WithMediaType.
type WithMediaType string

func (w WithMediaType) ApplyToMemoryBlob(b *Blob) {
	b.SetMediaType(string(w))
}

// WithSize is a MemoryBlobOption that sets the size of the Blob.
// When applied, it will set the size of the Blob to the int64 value of the WithSize.
// It is used to precalculate the size of the Blob, which can be useful for performance optimization.
type WithSize int64

func (w WithSize) ApplyToMemoryBlob(b *Blob) {
	b.SetPrecalculatedSize(int64(w))
}

// WithDigest is a MemoryBlobOption that sets the digest of the Blob.
// When applied, it will set the digest of the Blob to the string value of the WithDigest.
// It is used to precalculate the digest of the Blob, as well as to verify the integrity of the blob data
// on its read if provided.
type WithDigest string

func (w WithDigest) ApplyToMemoryBlob(b *Blob) {
	b.SetPrecalculatedDigest(string(w))
}
