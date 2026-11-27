package attestation_test

import (
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/internal/attestation"
	"ocm.software/open-component-model/bindings/go/repository"
)

// TestDiscoverAllFromRoot_ReportsEveryInTotoLayer walks a BuildKit image index and reports
// one attestation per in-toto layer, carrying the layer's predicate type and the platform
// of the image it attests, as the oci-index source. Metadata only: no statement blob is
// read.
func TestDiscoverAllFromRoot_ReportsEveryInTotoLayer(t *testing.T) {
	store := newStore(t)
	img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
	att := attestationFor(t, store, img, repository.PredicateTypeSPDX, slsaProvenance)
	root := pushJSON(t, store, ociImageSpecV1.MediaTypeImageIndex, ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{img, att},
	})

	out, err := attestation.DiscoverAllFromRoot(t.Context(), store, root)
	require.NoError(t, err)
	require.Len(t, out, 2, "both in-toto layers are reported, SBOM and provenance")

	predicates := make([]string, 0, len(out))
	for _, a := range out {
		assert.Equal(t, repository.AttestationSourceOCIIndex, a.Source)
		assert.Equal(t, "linux/amd64", a.Platform.String(), "the platform of the attested image is carried")
		assert.Equal(t, img.Digest.String(), a.Digest, "the subject is the image the attestation describes")
		assert.Equal(t, attestation.MediaTypeInTotoStatement, a.MediaType)
		assert.NotEmpty(t, a.ID, "the in-toto layer digest identifies the document")
		predicates = append(predicates, a.PredicateType)
	}
	assert.ElementsMatch(t, []string{repository.PredicateTypeSPDX, slsaProvenance}, predicates)
}

// TestDiscoverAllFromRoot_PlainManifestYieldsNothing checks that a root that is a plain
// image manifest (no index to walk) returns nothing without error.
func TestDiscoverAllFromRoot_PlainManifestYieldsNothing(t *testing.T) {
	store := newStore(t)
	img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)

	out, err := attestation.DiscoverAllFromRoot(t.Context(), store, img)
	require.NoError(t, err)
	assert.Nil(t, out, "a plain image manifest carries no BuildKit index")
}
