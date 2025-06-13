package blob

import (
	"io"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// NewDirectReadOnlyBlob is a form of memory-blob creation. See [inmemory.New] for more information.
//
// Deprecated: NewDirectReadOnlyBlob is deprecated and will be removed in a future release.
// It is recommended to use [inmemory.New] instead, which provides more flexibility and options for creating memory blobs.
func NewDirectReadOnlyBlob(r io.Reader) *DirectReadOnlyBlob {
	return (*DirectReadOnlyBlob)(inmemory.New(r))
}

// DirectReadOnlyBlob is an alias for [inmemory.Blob].
//
// Deprecated: DirectReadOnlyBlob is deprecated and will be removed in a future release.
// It is recommended to use [inmemory.Blob] instead, which provides more flexibility and options for creating memory blobs.
type DirectReadOnlyBlob inmemory.Blob

func (d *DirectReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	return (*inmemory.Blob)(d).ReadCloser()
}
