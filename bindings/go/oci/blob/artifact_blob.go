package blob

import (
	"fmt"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory/cache"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
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
	// Packing changes access metadata, not the representation of these bytes.
	ociLayout bool

	digestMu   sync.RWMutex
	byteDigest string
}

// NewArtifactBlobWithMediaType creates a new ArtifactBlob instance with the given artifact,
// blob data, and media type.
func NewArtifactBlobWithMediaType(artifact descriptor.Artifact, b blob.ReadOnlyBlob, mediaType string) (*ArtifactBlob, error) {
	if mediaType == "" {
		if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
			mediaType, _ = mediaTypeAware.MediaType()
		}
	}

	result := &ArtifactBlob{
		ReadOnlyBlob: b,
		Artifact:     artifact,
		mediaType:    mediaType,
		ociLayout:    isOCILayout(artifact, b, mediaType),
	}

	// Normalized OCI manifest digests do not describe the bytes of a layout archive.
	if resource, ok := artifact.(*descriptor.Resource); ok && !result.ociLayout {
		if digAware, ok := b.(blob.DigestAware); ok {
			if blobDig, ok := digAware.Digest(); ok {
				if isByteDigest(resource.Digest) {
					dig, err := digestSpecToDigest(resource.Digest)
					if err != nil {
						return nil, fmt.Errorf("failed to parse digest spec from resource: %w", err)
					}
					if dig != digest.Digest(blobDig) {
						return nil, fmt.Errorf("resource blob digest mismatch: resource %s vs blob %s", resource.Digest.Value, blobDig)
					}
				} else if resource.Digest == nil {
					resource.Digest = digestSpecFromDigest(digest.Digest(blobDig))
					resource.Digest.NormalisationAlgorithm = internaldigest.GenericBlobDigestV1
				}
			}
		}
	}

	return result, nil
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

// Digest returns a checksum of the blob's bytes, not its normalized resource digest.
func (r *ArtifactBlob) Digest() (string, bool) {
	r.digestMu.RLock()
	dig := r.byteDigest
	r.digestMu.RUnlock()
	if dig != "" {
		return dig, true
	}
	if digAware, ok := r.ReadOnlyBlob.(blob.DigestAware); ok {
		if dig, known := digAware.Digest(); known && dig != "" {
			return dig, true
		}
	}
	return r.resourceByteDigest()
}

func (r *ArtifactBlob) resourceByteDigest() (string, bool) {
	if resource, ok := r.Artifact.(*descriptor.Resource); ok && !r.ociLayout && isByteDigest(resource.Digest) {
		dig, err := digestSpecToDigest(resource.Digest)
		if err == nil {
			return dig.String(), true
		}
	}
	return "", false
}

func isByteDigest(dig *descriptor.Digest) bool {
	return dig != nil && dig.Value != "" &&
		(dig.NormalisationAlgorithm == "" || dig.NormalisationAlgorithm == internaldigest.GenericBlobDigestV1)
}

func isOCILayout(artifact descriptor.Artifact, b blob.ReadOnlyBlob, mediaType string) bool {
	isLayout := func(mediaType string) bool {
		return mediaType == layout.MediaTypeOCIImageLayoutTarV1 || mediaType == layout.MediaTypeOCIImageLayoutTarGzipV1
	}
	if isLayout(mediaType) {
		return true
	}
	if wrapped, ok := b.(*ArtifactBlob); ok && wrapped.ociLayout {
		return true
	}
	if aware, ok := b.(blob.MediaTypeAware); ok {
		if mediaType, _ := aware.MediaType(); isLayout(mediaType) {
			return true
		}
	}
	if resource, ok := artifact.(*descriptor.Resource); ok {
		switch access := resource.Access.(type) {
		case *descriptor.LocalBlob:
			return isLayout(access.MediaType)
		case *v2.LocalBlob:
			return isLayout(access.MediaType)
		}
		var access v2.LocalBlob
		if err := v2.Scheme.Convert(resource.Access, &access); err == nil {
			return isLayout(access.MediaType)
		}
	}
	return false
}

// HasPrecalculatedDigest reports whether a byte checksum is known without reading the blob.
func (r *ArtifactBlob) HasPrecalculatedDigest() bool {
	r.digestMu.RLock()
	known := r.byteDigest != ""
	r.digestMu.RUnlock()
	if known {
		return true
	}
	if precalculated, ok := r.ReadOnlyBlob.(blob.DigestPrecalculatable); ok && precalculated.HasPrecalculatedDigest() {
		return true
	}
	_, ok := r.resourceByteDigest()
	return ok
}

// SetPrecalculatedDigest stores a byte checksum without modifying signed resource metadata.
// It panics if dig is neither empty nor a valid digest.
func (r *ArtifactBlob) SetPrecalculatedDigest(dig string) {
	if dig != "" {
		if _, err := digest.Parse(dig); err != nil {
			panic(err)
		}
	}
	r.digestMu.Lock()
	r.byteDigest = dig
	r.digestMu.Unlock()
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

	// Caching has already verified the byte checksum. Do not re-default or
	// re-interpret the shared resource metadata from the buffered representation.
	return &ArtifactBlob{
		ReadOnlyBlob: inMemoryBlob,
		Artifact:     r.Artifact,
		mediaType:    r.mediaType,
		ociLayout:    r.ociLayout,
	}, nil
}

// Interface implementations
var (
	_ blob.ReadOnlyBlob   = (*ArtifactBlob)(nil)
	_ blob.SizeAware      = (*ArtifactBlob)(nil)
	_ blob.DigestAware    = (*ArtifactBlob)(nil)
	_ blob.MediaTypeAware = (*ArtifactBlob)(nil)
)
