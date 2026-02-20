package introspection

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// IsOCICompliantManifest checks if a descriptor describes a manifest that is recognizable by OCI.
func IsOCICompliantManifest(desc ociImageSpecV1.Descriptor) bool {
	return IsOCICompliantMediaType(desc.MediaType)
}

// IsOCICompliantMediaType checks if a media type is recognized by OCI.
func IsOCICompliantMediaType(mediaType string) bool {
	switch mediaType {
	// TODO(jakobmoellerdev): currently only Image Indexes and OCI manifests are supported,
	//  but we may want to extend this down the line with additional media types such as docker manifests.
	case ociImageSpecV1.MediaTypeImageManifest,
		ociImageSpecV1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
}
