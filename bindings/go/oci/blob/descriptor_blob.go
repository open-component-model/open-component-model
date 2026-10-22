package blob

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

// NewDescriptorBlob creates a new DescriptorBlob.
// This is a blob that is backed by a reader (with the content) as well as an OCI descriptor.
// The descriptor is used to report data such as a precalculated digest and size without having to introspect the data.
// At the same time the data that is given is verified against the descriptor.
// The blob owns data: closing the blob or a reader returned by ReadCloser closes it.
func NewDescriptorBlob(data io.ReadCloser, descriptor ociImageSpecV1.Descriptor) *DescriptorBlob {
	return &DescriptorBlob{
		content:    data,
		close:      sync.OnceValue(data.Close),
		descriptor: descriptor,
	}
}

var (
	_ blob.ReadOnlyBlob   = (*DescriptorBlob)(nil)
	_ blob.SizeAware      = (*DescriptorBlob)(nil)
	_ blob.DigestAware    = (*DescriptorBlob)(nil)
	_ blob.MediaTypeAware = (*DescriptorBlob)(nil)
	_ io.Closer           = (*DescriptorBlob)(nil)
)

type DescriptorBlob struct {
	content io.ReadCloser
	// close closes content at most once, whether through the blob or its reader.
	close      func() error
	descriptor ociImageSpecV1.Descriptor
}

// Close releases the underlying content, so an owner can release a blob that was never
// (fully) read. The content is closed only once; later calls return the first result.
func (c *DescriptorBlob) Close() error {
	return c.close()
}

// MediaType returns the media type of the blob by returning the descriptor's media type.
func (c *DescriptorBlob) MediaType() (string, bool) {
	return c.descriptor.MediaType, true
}

// Digest returns the digest of the blob by returning the descriptor's digest.
func (c *DescriptorBlob) Digest() (string, bool) {
	return c.descriptor.Digest.String(), true
}

// HasPrecalculatedDigest returns true as the digest is precalculated in the descriptor already.
func (c *DescriptorBlob) HasPrecalculatedDigest() bool {
	return true
}

// SetPrecalculatedDigest sets the digest in the descriptor.
func (c *DescriptorBlob) SetPrecalculatedDigest(d string) {
	c.descriptor.Digest = digest.Digest(d)
}

// Size returns the size of the blob by returning the descriptor's size.
func (c *DescriptorBlob) Size() int64 {
	return c.descriptor.Size
}

// HasPrecalculatedSize returns true as the size is precalculated in the descriptor already.
func (c *DescriptorBlob) HasPrecalculatedSize() bool {
	return true
}

// SetPrecalculatedSize sets the size in the descriptor.
func (c *DescriptorBlob) SetPrecalculatedSize(size int64) {
	c.descriptor.Size = size
}

// ReadCloser returns the data behind the content.
func (c *DescriptorBlob) ReadCloser() (io.ReadCloser, error) {
	return newCloseableVerifyReader(c.content, c.close, c.descriptor), nil
}

// closeableVerifyReader is a wrapper around content.VerifyReader that allows closing the underlying reader.
// additionally it verifies the digest of the content when closing.
type closeableVerifyReader struct {
	reader *content.VerifyReader
	close  func() error
}

func newCloseableVerifyReader(r io.Reader, closeFn func() error, descriptor ociImageSpecV1.Descriptor) *closeableVerifyReader {
	return &closeableVerifyReader{
		reader: content.NewVerifyReader(r, descriptor),
		close:  closeFn,
	}
}

func (c *closeableVerifyReader) Read(p []byte) (n int, err error) {
	return c.reader.Read(p)
}

// Close verifies the content read so far and always closes the underlying reader, also
// when verification fails (e.g. after a partial read).
func (c *closeableVerifyReader) Close() error {
	var errs []error
	if err := c.reader.Verify(); err != nil {
		errs = append(errs, fmt.Errorf("failed to verify digest verification reader in descriptor blob: %w", err))
	}
	if err := c.close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close digest verification reader in descriptor blob: %w", err))
	}
	return errors.Join(errs...)
}
