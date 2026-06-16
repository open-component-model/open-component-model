package introspection

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// Docker manifest media types as defined by the Docker distribution spec.
// oras-go/v2 defines these in internal/docker/mediatype.go (not importable),
// so we redeclare them here.
const (
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// IsOCICompliantManifest checks if a descriptor describes a manifest that is recognizable by OCI.
func IsOCICompliantManifest(desc ociImageSpecV1.Descriptor) bool {
	return IsOCICompliantMediaType(desc.MediaType)
}

// IsOCICompliantMediaType checks if a media type is recognized by OCI.
func IsOCICompliantMediaType(mediaType string) bool {
	switch mediaType {
	case ociImageSpecV1.MediaTypeImageManifest,
		ociImageSpecV1.MediaTypeImageIndex,
		MediaTypeDockerManifest,
		MediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}
