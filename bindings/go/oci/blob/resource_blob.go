package blob

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/digest/v1"
)

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
func NewResourceBlobWithMediaType(resource *descriptor.Resource, b blob.ReadOnlyBlob, mediaType string) (*ResourceBlob, error) {
	if sizeAware, ok := b.(blob.SizeAware); ok {
		blobSize := sizeAware.Size()
		if resource.Size == 0 && blobSize > blob.SizeUnknown {
			resource.Size = blobSize
		}
		if resource.Size != blobSize && blobSize > blob.SizeUnknown {
			return nil, fmt.Errorf("resource blob size mismatch: resource %d vs blob %d", resource.Size, blobSize)
		}
	}

	if mediaType == "" {
		if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
			mediaType, _ = mediaTypeAware.MediaType()
		}
	}

	if resource.Digest == nil {
		if digAware, ok := b.(blob.DigestAware); ok {
			if dig, ok := digAware.Digest(); ok {
				digSpec, err := digestSpec(dig)
				if err != nil {
					return nil, fmt.Errorf("failed to parse digest spec from blob: %w", err)
				}
				resource.Digest = digSpec
			}
		}
	} else {
		dig, err := digestSpecToDigest(resource.Digest)
		if err != nil {
			return nil, fmt.Errorf("failed to parse digest spec from resource: %w", err)
		}
		if digAware, ok := b.(blob.DigestAware); ok {
			if blobDig, ok := digAware.Digest(); ok {
				if dig != digest.Digest(blobDig) {
					return nil, fmt.Errorf("resource blob digest mismatch: resource %s vs blob %s", resource.Digest.Value, blobDig)
				}
			}
		}
	}

	return &ResourceBlob{
		ReadOnlyBlob: b,
		Resource:     resource,
		mediaType:    mediaType,
	}, nil
}

func NewResourceBlob(resource *descriptor.Resource, blob blob.ReadOnlyBlob) (*ResourceBlob, error) {
	return NewResourceBlobWithMediaType(resource, blob, "")
}

// MediaType returns the media type of the blob and a boolean indicating whether
// the media type is available. This is important for OCI compatibility and
// proper handling of different types of content.
func (r *ResourceBlob) MediaType() (string, bool) {
	return r.mediaType, r.mediaType != ""
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
	dig, err := digestSpecToDigest(r.Resource.Digest)
	if err != nil {
		return "", false
	}
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

	d, err := digestSpec(dig)
	if err != nil {
		panic(err)
	}
	r.Resource.Digest = d
}

func digestSpec(dig string) (*descriptor.Digest, error) {
	if dig == "" {
		return nil, nil
	}
	d, err := digest.Parse(dig)
	if err != nil {
		return nil, err
	}
	return digestSpecFromDigest(d), nil
}

func digestSpecFromDigest(dig digest.Digest) *descriptor.Digest {
	return &descriptor.Digest{
		Value:         dig.Encoded(),
		HashAlgorithm: v1.ReverseSHAMapping[dig.Algorithm()],
	}
}

func digestSpecToDigest(dig *descriptor.Digest) (digest.Digest, error) {
	algo, ok := v1.SHAMapping[dig.HashAlgorithm]
	if !ok {
		return "", fmt.Errorf("invalid hash algorithm: %s", dig.HashAlgorithm)
	}

	return digest.NewDigestFromEncoded(algo, dig.Value), nil
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
