package blob

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory/cache"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
)

// ArtifactBlob represents a blob of data that is associated with an OCM Source or Resource .
// It implements various interfaces to provide blob-related functionality like
// reading data, getting size, digest, and media type. This type is particularly
// useful when working with OCI (Open Container Initiative) artifacts in the OCM
// context, as it bridges the gap between OCM resources and OCI blobs.
type ArtifactBlob struct {
	blob.ReadOnlyBlob
	descriptor.Artifact
	mediaType string
}

// NewArtifactBlobWithMediaType creates a new ArtifactBlob instance with the given artifact,
// blob data, and media type.
func NewArtifactBlobWithMediaType(artifact descriptor.Artifact, b blob.ReadOnlyBlob, mediaType string) (*ArtifactBlob, error) {
	if mediaType == "" {
		if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
			mediaType, _ = mediaTypeAware.MediaType()
		}
	}

	// lets do additional defaulting and verification of the resulting blob
	// if we have a resource, because a resource contains more data than a generic artifact
	if resource, ok := artifact.(*descriptor.Resource); ok {
		if digAware, ok := b.(blob.DigestAware); ok {
			if blobDig, ok := digAware.Digest(); ok {
				if resource.Digest != nil {
					// if we have a digest in the resource and in the blob, we need to verify that
					// they don't mismatch with each other
					dig, err := digestSpecToDigest(resource.Digest)
					if err != nil {
						return nil, fmt.Errorf("failed to parse digest spec from resource: %w", err)
					}
					if dig != digest.Digest(blobDig) {
						return nil, fmt.Errorf("resource blob digest mismatch: resource %s vs blob %s", resource.Digest.Value, blobDig)
					}
				} else {
					resource.Digest = digestSpecFromDigest(digest.Digest(blobDig))
				}
			}
		}
	}

	return &ArtifactBlob{
		ReadOnlyBlob: b,
		Artifact:     artifact,
		mediaType:    mediaType,
	}, nil
}

func NewArtifactBlob(artifact descriptor.Artifact, blob blob.ReadOnlyBlob) (*ArtifactBlob, error) {
	return NewArtifactBlobWithMediaType(artifact, blob, "")
}

// MediaType returns the media type of the blob and a boolean indicating whether
// the media type is available. This is important for OCI compatibility and
// proper handling of different types of content.
func (r *ArtifactBlob) MediaType() (string, bool) {
	return r.mediaType, r.mediaType != ""
}

// Digest returns the digest of the blob's content and a boolean indicating whether
// the digest is available. The digest is calculated from the resource's digest value
// and hash algorithm. If the resource's digest is nil or the hash algorithm is not
// supported, it returns an empty string and false. The method converts the OCM hash
// algorithm to the corresponding OCI digest algorithm using HashAlgorithmConversionTable.
func (r *ArtifactBlob) Digest() (string, bool) {
	switch typed := r.Artifact.(type) {
	case *descriptor.Resource:
		if typed.Digest == nil {
			if digAware, ok := r.ReadOnlyBlob.(blob.DigestAware); ok {
				return digAware.Digest()
			}
			return "", false
		}
		dig, err := digestSpecToDigest(typed.Digest)
		if err != nil {
			return "", false
		}
		return dig.String(), true
	case *descriptor.Source:
		if digAware, ok := r.ReadOnlyBlob.(blob.DigestAware); ok {
			return digAware.Digest()
		}
	}
	return "", false
}

// HasPrecalculatedDigest indicates whether the blob has a pre-calculated digest.
// This is always true for ArtifactBlob as it uses the digest from the associated resource.
func (r *ArtifactBlob) HasPrecalculatedDigest() bool {
	switch typed := r.Artifact.(type) {
	case *descriptor.Resource:
		return typed.Digest != nil && typed.Digest.Value != ""
	default:
		return false
	}
}

// SetPrecalculatedDigest sets the pre-calculated digest value for the resource.
// This method allows updating the digest value when it's known beforehand.
// Note that this method only updates the digest value and assumes the normalisation algorithm
// is already set correctly in the resource.
func (r *ArtifactBlob) SetPrecalculatedDigest(dig string) {
	resource, ok := r.Artifact.(*descriptor.Resource)
	if !ok {
		return
	}
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{}
	}
	d, err := digestSpec(dig)
	if err != nil {
		panic(err)
	}
	resource.Digest = d
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
		HashAlgorithm: internaldigest.ReverseSHAMapping[dig.Algorithm()],
	}
}

func digestSpecToDigest(dig *descriptor.Digest) (digest.Digest, error) {
	algo, ok := internaldigest.SHAMapping[dig.HashAlgorithm]
	if !ok {
		return "", fmt.Errorf("invalid hash algorithm: %s", dig.HashAlgorithm)
	}

	return digest.NewDigestFromEncoded(algo, dig.Value), nil
}

// Size returns the size of the blob in bytes. This is obtained directly from
// the associated resource's size field.
func (r *ArtifactBlob) Size() int64 {
	size := blob.SizeUnknown
	if sizeAware, ok := r.ReadOnlyBlob.(blob.SizeAware); ok {
		if blobSize := sizeAware.Size(); blobSize != size {
			size = blobSize
		}
	}

	return size
}

// OCIDescriptor returns an OCI descriptor for the blob. This is particularly
// useful when working with OCI registries and artifacts, as it provides the
// necessary metadata in the OCI format. The descriptor includes the media type,
// digest, and size of the blob.
func (r *ArtifactBlob) OCIDescriptor() ociImageSpecV1.Descriptor {
	dig, _ := r.Digest()
	return ociImageSpecV1.Descriptor{
		MediaType: r.mediaType,
		Digest:    digest.Digest(dig),
		Size:      r.Size(),
	}
}

// Buffer creates a new ArtifactBlob with an in-memory buffered blob.
// It can be used to covert ArtifactBlob with unknown size or digest to a new instance where those fields a set.
func (r *ArtifactBlob) Buffer() (result *ArtifactBlob, err error) {
	inMemoryBlob, err := cache.Cache(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create in-memory eagerly cached blob from ReadOnlyBlob: %w", err)
	}

	// Reuse existing Artifact, but replace the ReadOnlyBlob with the in-memory buffered one.
	return NewArtifactBlob(r.Artifact, inMemoryBlob)
}

// Interface implementations
var (
	_ blob.ReadOnlyBlob   = (*ArtifactBlob)(nil)
	_ blob.SizeAware      = (*ArtifactBlob)(nil)
	_ blob.DigestAware    = (*ArtifactBlob)(nil)
	_ blob.MediaTypeAware = (*ArtifactBlob)(nil)
)
