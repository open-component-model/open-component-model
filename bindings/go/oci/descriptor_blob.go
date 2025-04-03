package oci

import (
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

// NewDescriptorBlob creates a new DescriptorBlob.
// This is a blob that is backed by a reader (with the content) as well as an OCI descriptor.
// The descriptor is used to report data such as a precalculated digest and size without having to introspect the data.
// At the same time the data that is given is verified against the descriptor.
func NewDescriptorBlob(data io.Reader, descriptor ociImageSpecV1.Descriptor) *DescriptorBlob {
	var reader io.ReadCloser
	if closer, ok := data.(io.ReadCloser); ok {
		reader = closer
	} else {
		reader = io.NopCloser(data)
	}

	return &DescriptorBlob{
		content:    reader,
		descriptor: descriptor,
	}
}

var (
	_ blob.ReadOnlyBlob   = (*DescriptorBlob)(nil)
	_ blob.SizeAware      = (*DescriptorBlob)(nil)
	_ blob.DigestAware    = (*DescriptorBlob)(nil)
	_ blob.MediaTypeAware = (*DescriptorBlob)(nil)
)

type DescriptorBlob struct {
	content    io.ReadCloser
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

// ReadCloser returns the data behind the content.
func (c *DescriptorBlob) ReadCloser() (io.ReadCloser, error) {
	return newCloseableVerifyReader(c.content, c.descriptor), nil
}

// closeableVerifyReader is a wrapper around content.VerifyReader that allows closing the underlying reader.
// additionally it verifies the digest of the content when closing.
type closeableVerifyReader struct {
	reader *content.VerifyReader
	close  func() error
}

func newCloseableVerifyReader(r io.ReadCloser, descriptor ociImageSpecV1.Descriptor) *closeableVerifyReader {
	return &closeableVerifyReader{
		reader: content.NewVerifyReader(r, descriptor),
		close:  r.Close,
	}
}

func (c *closeableVerifyReader) Read(p []byte) (n int, err error) {
	return c.reader.Read(p)
}

func (c *closeableVerifyReader) Close() error {
	if c.close != nil {
		if err := c.close(); err != nil {
			return fmt.Errorf("failed to close digest verification reader in descriptor blob: %w", err)
		}
	}
	if err := c.reader.Verify(); err != nil {
		return fmt.Errorf("failed to verify digest verification reader in descriptor blob: %w", err)
	}

	return nil
}
