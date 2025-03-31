package blob

import "io"

// DirectReadOnlyBlob is a read-only blob that reads from an io.Reader.
type DirectReadOnlyBlob struct {
	*EagerBufferedReader
}

func (d *DirectReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	return d, nil
}

// NewDirectReadOnlyBlob forwards a given io.Reader to be able to be used as a ReadOnlyBlob.
// It does this by wrapping the io.Reader in a EagerBufferedReader, to allow for catching information such as
// the digest and size of the blob.
// Note that this should only be used if no sizing and digest information is present.
func NewDirectReadOnlyBlob(r io.Reader) *DirectReadOnlyBlob {
	return &DirectReadOnlyBlob{EagerBufferedReader: NewEagerBufferedReader(r)}
}

var _ ReadOnlyBlob = &DirectReadOnlyBlob{}
