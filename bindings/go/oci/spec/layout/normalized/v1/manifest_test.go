package v1_test

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"

	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

func TestBuildNormalizedManifest(t *testing.T) {
	normLayer := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON,
		Digest:    "sha256:1111",
		Size:      10,
	}
	m := layout.BuildNormalizedManifest(normLayer, "example.org/comp", "1.0.0")

	assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, m.MediaType)
	assert.Equal(t, ocidescriptor.ArtifactTypeNormalizedDescriptor, m.ArtifactType)
	assert.Equal(t, ociImageSpecV1.DescriptorEmptyJSON, m.Config)
	assert.Equal(t, []ociImageSpecV1.Descriptor{normLayer}, m.Layers)
	assert.Nil(t, m.Subject)
	assert.Equal(t, layout.LayoutVersion, m.Annotations[layout.AnnotationLayoutVersion])
	assert.Equal(t, layout.NormalisationAlgorithm, m.Annotations[layout.AnnotationNormalisationAlgo])
	assert.Equal(t, 2, m.SchemaVersion)
}

func TestBuildAccessManifest(t *testing.T) {
	subject := ociImageSpecV1.Descriptor{Digest: "sha256:1111"}
	config := ociImageSpecV1.Descriptor{MediaType: "application/vnd.ocm.software.component.config.v1+json", Digest: "sha256:2222"}
	descLayer := ociImageSpecV1.Descriptor{MediaType: ocidescriptor.MediaTypeComponentDescriptorJSON, Digest: "sha256:3333"}
	blob := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: "sha256:4444"}

	a := layout.BuildAccessManifest(subject, config, descLayer, []ociImageSpecV1.Descriptor{blob}, "example.org/comp", "1.0.0")

	assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, a.MediaType)
	assert.Equal(t, ocidescriptor.ArtifactTypeAccessDescriptor, a.ArtifactType)
	require := assert.New(t)
	require.NotNil(a.Subject)
	assert.Equal(t, subject, *a.Subject)
	assert.Equal(t, config, a.Config)
	assert.Equal(t, []ociImageSpecV1.Descriptor{descLayer, blob}, a.Layers)
	assert.Equal(t, layout.LayoutVersion, a.Annotations[layout.AnnotationLayoutVersion])
}
