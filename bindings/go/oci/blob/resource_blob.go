package blob

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// HashAlgorithmConversionTable maps OCM hash algorithm names to their corresponding
// OCI digest algorithms. This table is used to convert between OCM and OCI digest formats.
// If the hash algorithm is empty, it defaults to the canonical algorithm (SHA-256).
var HashAlgorithmConversionTable = map[string]digest.Algorithm{
	"sha256": digest.SHA256,
	"sha384": digest.SHA384,
	"sha512": digest.SHA512,
	"":       digest.Canonical,
}

// ReverseHashAlgorithmConversionTable maps OCM digest algorithms to their
// corresponding hash algorithm names. This is the reverse of HashAlgorithmConversionTable.
// It is used to convert from OCI digest algorithms back to OCM hash algorithm names.
var ReverseHashAlgorithmConversionTable = map[digest.Algorithm]string{
	digest.SHA256: "sha256",
	digest.SHA384: "sha384",
	digest.SHA512: "sha512",
}

// ResourceBlob represents a blob of data that is associated with an OCM resource.
// It implements various interfaces to provide blob-related functionality like
// reading data, getting size, digest, and media type. This type is particularly
// useful when working with OCI (Open Container Initiative) artifacts in the OCM
// context, as it bridges the gap between OCM resources and OCI blobs.
type ResourceBlob struct {
	blob.ReadOnlyBlob
	*descriptor.Resource
	mediaType string
}

// NewResourceBlobWithMediaType creates a new ResourceBlob instance with the given resource,
// blob data, and media type. This constructor ensures that all necessary
// information is properly initialized for the ResourceBlob to function correctly.
func NewResourceBlobWithMediaType(resource *descriptor.Resource, b blob.ReadOnlyBlob, mediaType string) *ResourceBlob {
	if sizeAware, ok := b.(blob.SizeAware); ok {
		if sizeFromBlob := sizeAware.Size(); sizeFromBlob > blob.SizeUnknown {
			if resource.Size == 0 {
				resource.Size = sizeFromBlob
			}

			if resource.Size != sizeFromBlob {
				panic(fmt.Sprintf("resource blob size mismatch: %d vs %d", resource.Size, sizeFromBlob))
			}
		}
	}

	return &ResourceBlob{
		ReadOnlyBlob: b,
		Resource:     resource,
		mediaType:    mediaType,
	}
}

func NewResourceBlob(resource *descriptor.Resource, blob blob.ReadOnlyBlob) *ResourceBlob {
	return NewResourceBlobWithMediaType(resource, blob, "")
}

// MediaType returns the media type of the blob and a boolean indicating whether
// the media type is available. This is important for OCI compatibility and
// proper handling of different types of content.
func (r *ResourceBlob) MediaType() (string, bool) {
	return r.mediaType, true
}

// Digest returns the digest of the blob's content and a boolean indicating whether
// the digest is available. The digest is calculated from the resource's digest value
// and hash algorithm. If the resource's digest is nil or the hash algorithm is not
// supported, it returns an empty string and false. The method converts the OCM hash
// algorithm to the corresponding OCI digest algorithm using HashAlgorithmConversionTable.
func (r *ResourceBlob) Digest() (string, bool) {
	if r.Resource.Digest == nil {
		return "", false
	}

	algo, ok := HashAlgorithmConversionTable[r.Resource.Digest.HashAlgorithm]
	if !ok {
		return "", false
	}

	dig := digest.NewDigestFromEncoded(algo, r.Resource.Digest.Value)
	return dig.String(), true
}

// HasPrecalculatedDigest indicates whether the blob has a pre-calculated digest.
// This is always true for ResourceBlob as it uses the digest from the associated resource.
func (r *ResourceBlob) HasPrecalculatedDigest() bool {
	return r.Resource.Digest != nil && r.Resource.Digest.Value != ""
}

// SetPrecalculatedDigest sets the pre-calculated digest value for the resource.
// This method allows updating the digest value when it's known beforehand.
// Note that this method only updates the digest value and assumes the normalisation algorithm
// is already set correctly in the resource.
func (r *ResourceBlob) SetPrecalculatedDigest(dig string) {
	if r.Resource.Digest == nil {
		r.Resource.Digest = &descriptor.Digest{}
	}

	d, err := digest.Parse(dig)
	if err != nil {
		panic(err)
	}

	r.Resource.Digest.Value = d.Encoded()
	r.Resource.Digest.HashAlgorithm = ReverseHashAlgorithmConversionTable[d.Algorithm()]
}

// Size returns the size of the blob in bytes. This is obtained directly from
// the associated resource's size field.
func (r *ResourceBlob) Size() int64 {
	return r.Resource.Size
}

// HasPrecalculatedSize indicates whether the blob has a pre-calculated size.
// This is always true for ResourceBlob as it uses the size from the associated resource.
func (r *ResourceBlob) HasPrecalculatedSize() bool {
	return r.Resource.Size > blob.SizeUnknown
}

// SetPrecalculatedSize sets the pre-calculated size value for the resource.
// This method allows updating the size value when it's known beforehand.
func (r *ResourceBlob) SetPrecalculatedSize(size int64) {
	r.Resource.Size = size
}

// OCIDescriptor returns an OCI descriptor for the blob. This is particularly
// useful when working with OCI registries and artifacts, as it provides the
// necessary metadata in the OCI format. The descriptor includes the media type,
// digest, and size of the blob.
func (r *ResourceBlob) OCIDescriptor() ociImageSpecV1.Descriptor {
	dig, _ := r.Digest()
	return ociImageSpecV1.Descriptor{
		MediaType: r.mediaType,
		Digest:    digest.Digest(dig),
		Size:      r.Size(),
	}
}

// Interface implementations
var (
	_ blob.ReadOnlyBlob   = (*ResourceBlob)(nil)
	_ blob.SizeAware      = (*ResourceBlob)(nil)
	_ blob.DigestAware    = (*ResourceBlob)(nil)
	_ blob.MediaTypeAware = (*ResourceBlob)(nil)
)
