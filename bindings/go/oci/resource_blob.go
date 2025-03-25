package oci

import (
	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

type ResourceBlob struct {
	blob.ReadOnlyBlob
	*descriptor.Resource
	mediaType string
}

func NewResourceBlob(resource *descriptor.Resource, blob blob.ReadOnlyBlob, mediaType string) *ResourceBlob {
	return &ResourceBlob{
		ReadOnlyBlob: blob,
		Resource:     resource,
		mediaType:    mediaType,
	}
}

func (r *ResourceBlob) MediaType() (string, bool) {
	return r.mediaType, true
}

func (r *ResourceBlob) Digest() (string, bool) {
	// TODO support all algos
	return digest.NewDigestFromEncoded(digest.SHA256, r.Resource.Digest.Value).String(), true
}

func (r *ResourceBlob) HasPrecalculatedDigest() bool {
	return true
}

func (r *ResourceBlob) SetPrecalculatedDigest(digest string) {
	// TODO support all algos
	r.Resource.Digest.Value = digest
}

func (r *ResourceBlob) Size() int64 {
	return r.Resource.Size
}

func (r *ResourceBlob) HasPrecalculatedSize() bool {
	return true
}

func (r *ResourceBlob) SetPrecalculatedSize(size int64) {
	r.Resource.Size = size
}

func (r *ResourceBlob) OCIDescriptor() ociImageSpecV1.Descriptor {
	dig, _ := r.Digest()
	return ociImageSpecV1.Descriptor{
		MediaType: r.mediaType,
		Digest:    digest.Digest(dig),
		Size:      r.Size(),
	}
}

var (
	_ blob.ReadOnlyBlob   = (*ResourceBlob)(nil)
	_ blob.SizeAware      = (*ResourceBlob)(nil)
	_ blob.DigestAware    = (*ResourceBlob)(nil)
	_ blob.MediaTypeAware = (*ResourceBlob)(nil)
)
