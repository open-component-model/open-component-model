package oci

import (
	"encoding/json/v2"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// manifestHeader holds the fields of a manifest or index that tell what kind
// of referrer it is, without the content it lists.
type manifestHeader struct {
	MediaType    string `json:"mediaType"`
	ArtifactType string `json:"artifactType"`
	Config       struct {
		MediaType string `json:"mediaType"`
	} `json:"config"`
	Subject     *ociImageSpecV1.Descriptor `json:"subject"`
	Annotations map[string]string          `json:"annotations"`
}

func decodeManifestHeader(raw []byte) (manifestHeader, error) {
	var header manifestHeader
	err := json.Unmarshal(raw, &header)
	return header, err
}

// listedArtifactType returns the artifact type registries list content of
// mediaType with this header by. For an image manifest without artifactType
// that is the media type of its config.
func (h manifestHeader) listedArtifactType(mediaType string) string {
	if h.ArtifactType == "" && mediaType == ociImageSpecV1.MediaTypeImageManifest {
		return h.Config.MediaType
	}
	return h.ArtifactType
}
