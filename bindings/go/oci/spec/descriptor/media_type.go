package descriptor

// Media type constants for component descriptors
const (
	// MediaTypeComponentDescriptor is the base media type for OCM component descriptors
	MediaTypeComponentDescriptor = "application/vnd.ocm.software.component-descriptor"
	// MediaTypeComponentDescriptorV2 is the media type for version 2 of OCM component descriptors
	MediaTypeComponentDescriptorV2 = MediaTypeComponentDescriptor + ".v2"
)

const (
	// MediaTypeLegacyComponentDescriptorTar is the old mimetype for component-descriptor-blobs
	// that are stored as tar archives with a single expected file called LegacyComponentDescriptorTarFileName.
	// If the tar contains more than one file, the first file matching this file is used and all others are discarded.
	MediaTypeLegacyComponentDescriptorTar = MediaTypeComponentDescriptorYAML + "+tar"
	LegacyComponentDescriptorTarFileName  = "component-descriptor.yaml"

	// Legacy2ComponentDescriptorTarMimeType is the legacy mimetype for component-descriptor-blobs
	// that are stored as tar before the Open Sourcing of Open Component Model. Maintained for backwards compatibility ONLY.
	// Do not use
	mediaTypeLegacy2ComponentDescriptorTar = "application/vnd.gardener.cloud.cnudie.component-descriptor.v2+yaml+tar"
	mediaTypeLegacy3ComponentDescriptorTar = "application/vnd.oci.gardener.cloud.cnudie.component-descriptor.config.v2+yaml+tar"
)

// MediaTypeComponentDescriptorJSON is the mimetype for component-descriptor-blobs that are stored as plain JSON.
const (
	MediaTypeComponentDescriptorJSON       = MediaTypeComponentDescriptorV2 + "+json"
	MediaTypeLegacyComponentDescriptorJSON = "application/vnd.gardener.cloud.cnudie.component-descriptor.v2+json"
)

// MediaTypeComponentDescriptorJSON is the mimetype for component-descriptor-blobs
// that are stored as YAML.
const (
	MediaTypeComponentDescriptorYAML       = MediaTypeComponentDescriptorV2 + "+yaml"
	MediaTypeLegacyComponentDescriptorYAML = "application/vnd.gardener.cloud.cnudie.component-descriptor.v2+yaml"
)
