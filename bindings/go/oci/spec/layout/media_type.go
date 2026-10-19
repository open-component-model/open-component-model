package layout

import "strings"

// Media type constants for OCI image layouts
const (
	// MediaTypeOCIImageLayout is the media type for a complete OCI image layout
	// as per https://github.com/opencontainers/image-spec/blob/main/image-layout.md#oci-layout-file
	MediaTypeOCIImageLayout = "application/vnd.ocm.software.oci.layout"

	// MediaTypeOCIImageLayoutV1 is the media type for version 1 of OCI image layouts
	MediaTypeOCIImageLayoutV1        = MediaTypeOCIImageLayout + ".v1"
	MediaTypeOCIImageLayoutTarV1     = MediaTypeOCIImageLayoutV1 + "+tar"
	MediaTypeOCIImageLayoutTarGzipV1 = MediaTypeOCIImageLayoutV1 + "+tar+gzip"
)

// IsOCILayoutMediaType reports whether mediaType is an OCM OCI layout media type.
func IsOCILayoutMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, MediaTypeOCIImageLayout)
}
