package blob

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

// NewDescriptorBlob creates a new DescriptorBlob.
// This is a blob whose content is opened on demand and described by an OCI descriptor.
// The descriptor is used to report data such as a precalculated digest and size without having to introspect the data.
// Every call to ReadCloser calls open for a fresh stream and verifies it against the descriptor
// when the reader is closed. Nothing is held open between reads, so the blob needs no Close.
func NewDescriptorBlob(open func() (io.ReadCloser, error), descriptor ociImageSpecV1.Descriptor) *DescriptorBlob {
	return &DescriptorBlob{
		open:       open,
		descriptor: descriptor,
	}
}

// NewDescriptorBlobFromBytes creates a DescriptorBlob over data held in memory.
func NewDescriptorBlobFromBytes(data []byte, descriptor ociImageSpecV1.Descriptor) *DescriptorBlob {
	return NewDescriptorBlob(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}, descriptor)
}

var (
	_ blob.ReadOnlyBlob   = (*DescriptorBlob)(nil)
	_ blob.SizeAware      = (*DescriptorBlob)(nil)
	_ blob.DigestAware    = (*DescriptorBlob)(nil)
	_ blob.MediaTypeAware = (*DescriptorBlob)(nil)
)

type DescriptorBlob struct {
	open       func() (io.ReadCloser, error)
	descriptor ociImageSpecV1.Descriptor
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

// ReadCloser opens the content and returns a reader that verifies it against the
// descriptor when closed.
func (c *DescriptorBlob) ReadCloser() (io.ReadCloser, error) {
	data, err := c.open()
	if err != nil {
		return nil, fmt.Errorf("failed to open descriptor blob content: %w", err)
	}
	return newCloseableVerifyReader(data, data.Close, c.descriptor), nil
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
