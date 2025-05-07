package blob

import (
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// SourceBlob represents a blob of data that is associated with an OCM source.
// It implements various interfaces to provide blob-related functionality like
// reading data, getting size, digest, and media type. This type is particularly
// useful when working with OCI (Open Container Initiative) artifacts in the OCM
// context, as it bridges the gap between OCM sources and OCI blobs.
type SourceBlob struct {
	blob.ReadOnlyBlob
	*descriptor.Source
	mediaType string
}

// NewSourceBlobWithMediaType creates a new SourceBlob instance with the given resource,
// blob data, and media type. This constructor ensures that all necessary
// information is properly initialized for the SourceBlob to function correctly.
func NewSourceBlobWithMediaType(resource *descriptor.Source, b blob.ReadOnlyBlob, mediaType string) (*SourceBlob, error) {
	if mediaType == "" {
		if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
			mediaType, _ = mediaTypeAware.MediaType()
		}
	}

	return &SourceBlob{
		ReadOnlyBlob: b,
		Source:       resource,
		mediaType:    mediaType,
	}, nil
}

func NewSourceBlob(source *descriptor.Source, blob blob.ReadOnlyBlob) (*SourceBlob, error) {
	return NewSourceBlobWithMediaType(source, blob, "")
}

// MediaType returns the media type of the blob and a boolean indicating whether
// the media type is available. This is important for OCI compatibility and
// proper handling of different types of content.
func (r *SourceBlob) MediaType() (string, bool) {
	return r.mediaType, r.mediaType != ""
}

// Digest returns the digest of the blob's content and a boolean indicating whether
// the digest is available. The digest is calculated from the purely from the underlying blob
// as the Source does not yet have digest information in its specification
func (r *SourceBlob) Digest() (string, bool) {
	if digAware, ok := r.ReadOnlyBlob.(blob.DigestAware); ok {
		return digAware.Digest()
	}
	data, err := r.ReadCloser()
	if err != nil {
		return "", false
	}
	defer data.Close()
	if size := r.Size(); size > blob.SizeUnknown {
		if dig, err := digest.FromReader(io.LimitReader(data, size)); err == nil {
			return dig.String(), true
		}
	} else if dig, err := digest.FromReader(data); err == nil {
		return dig.String(), true
	}
	return "", false
}

func (r *SourceBlob) Size() int64 {
	if sizeAware, ok := r.ReadOnlyBlob.(blob.SizeAware); ok {
		return sizeAware.Size()
	}
	return blob.SizeUnknown
}

// OCIDescriptor returns an OCI descriptor for the blob. This is particularly
// useful when working with OCI registries and artifacts, as it provides the
// necessary metadata in the OCI format. The descriptor includes the media type,
// digest, and size of the blob.
func (r *SourceBlob) OCIDescriptor() ociImageSpecV1.Descriptor {
	dig, _ := r.Digest()
	return ociImageSpecV1.Descriptor{
		MediaType: r.mediaType,
		Digest:    digest.Digest(dig),
		Size:      r.Size(),
	}
}
