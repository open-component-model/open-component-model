package blob

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
)

// NewDescriptorBlob returns a blob reading its content from src that is described by an OCI
// descriptor: it reports the descriptor's digest, size and media type without introspecting
// the content, and verifies every read of the content against them.
func NewDescriptorBlob(src blob.ReadOnlyBlob, descriptor ociImageSpecV1.Descriptor) *verified.Blob {
	return verified.New(src, verified.Metadata{
		Digest:    descriptor.Digest,
		Size:      descriptor.Size,
		MediaType: descriptor.MediaType,
	})
}
