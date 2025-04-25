package introspection

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// IsOCICompliantManifest checks if a descriptor describes a manifest that is recognizable by OCI.
func IsOCICompliantManifest(desc ociImageSpecV1.Descriptor) bool {
	switch desc.MediaType {
	// TODO(jakobmoellerdev): currently only Image Indexes and OCI manifests are supported,
	//  but we may want to extend this down the line with additional media types such as docker manifests.
	case ociImageSpecV1.MediaTypeImageManifest,
		ociImageSpecV1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
}
