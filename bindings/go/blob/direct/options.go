package direct

type DirectBlobOption interface {
	ApplyToDirectBlob(*Blob)
}

// WithMediaType is a DirectBlobOption that sets the media type of the Blob.
// When applied, it will set the media type of the Blob to the string value of the WithMediaType.
type WithMediaType string

func (w WithMediaType) ApplyToDirectBlob(b *Blob) {
	if w != "" {
		b.SetMediaType(string(w))
	}
}

// WithSize is a DirectBlobOption that sets the size of the Blob.
// When applied, it will set the size of the Blob to the int64 value of the WithSize option statically.
type WithSize int64

func (w WithSize) ApplyToDirectBlob(b *Blob) {
	if w >= 0 {
		b.size = func() (int64, error) {
			return int64(w), nil
		}
	}
}
