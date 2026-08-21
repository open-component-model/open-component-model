package attestation_test

import (
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/attestation"
	"ocm.software/open-component-model/bindings/go/repository"
)

func TestToRepositoryPlatform(t *testing.T) {
	assert.Equal(t, repository.Platform{
		OS:           "windows",
		Architecture: "amd64",
		Variant:      "v2",
		OSVersion:    "10.0.20348.3692",
		OSFeatures:   []string{"win32k"},
	}, attestation.ToRepositoryPlatform(ociImageSpecV1.Platform{
		OS:           "windows",
		Architecture: "amd64",
		Variant:      "v2",
		OSVersion:    "10.0.20348.3692",
		OSFeatures:   []string{"win32k"},
	}))
}

func TestToRepositorySBOMs(t *testing.T) {
	layer := digest.FromString("layer")

	converted := attestation.ToRepositorySBOMs([]attestation.SBOM{{
		Platform:      ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"},
		Subject:       ociImageSpecV1.Descriptor{Digest: digest.FromString("subject")},
		Manifest:      ociImageSpecV1.Descriptor{Digest: digest.FromString("manifest")},
		Layer:         ociImageSpecV1.Descriptor{Digest: layer},
		PredicateType: repository.PredicateTypeSPDX,
		Name:          "sbom",
		Statement:     []byte(`{"predicate":{"spdxVersion":"SPDX-2.3"}}`),
		Predicate:     []byte(`{"spdxVersion":"SPDX-2.3"}`),
	}})

	require.Len(t, converted, 1)
	assert.Equal(t, repository.SBOM{
		Platform:      repository.Platform{OS: "linux", Architecture: "arm64"},
		ID:            layer.String(),
		Name:          "sbom",
		PredicateType: repository.PredicateTypeSPDX,
		Data:          []byte(`{"spdxVersion":"SPDX-2.3"}`),
	}, converted[0], "the in-toto layer digest becomes the id, the oci detail is dropped")
	assert.Equal(t, repository.MediaTypeSPDXJSON, converted[0].MediaType())
}

func TestToRepositorySBOMs_Empty(t *testing.T) {
	assert.Empty(t, attestation.ToRepositorySBOMs(nil))
}
