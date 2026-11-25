package attestation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/repository"
)

func TestTypeAliases(t *testing.T) {
	cases := []struct {
		name string
		att  repository.Attestation
		want []string
	}{
		{"spdx predicate", repository.Attestation{PredicateType: repository.PredicateTypeSPDX}, []string{"spdx", "sbom"}},
		{"cyclonedx media", repository.Attestation{MediaType: repository.MediaTypeCycloneDXJSON}, []string{"cyclonedx", "sbom"}},
		{"openvex predicate versioned", repository.Attestation{PredicateType: predicateOpenVEX + "/v0.2.0"}, []string{"openvex", "vex"}},
		{"slsa provenance", repository.Attestation{PredicateType: predicateSLSA + "v1"}, []string{"slsa", "provenance"}},
		{"cosign signature artifact type", repository.Attestation{ArtifactType: artifactCosignSig}, []string{"cosign", "signature"}},
		{"notary signature artifact type", repository.Attestation{ArtifactType: artifactNotation}, []string{"notation", "signature"}},
		{"ocm-local sbom resource type", repository.Attestation{ArtifactType: "sbom"}, []string{"sbom"}},
		{"unknown has no aliases", repository.Attestation{ArtifactType: "application/vnd.acme.thing"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, TypeAliases(c.att))
		})
	}
}

func TestFilterByType(t *testing.T) {
	const mediaInTotoStatement = "application/vnd.in-toto+json"
	spdx := repository.Attestation{PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement}
	cyclonedx := repository.Attestation{ArtifactType: repository.MediaTypeCycloneDXJSON, MediaType: repository.MediaTypeCycloneDXJSON}
	openvex := repository.Attestation{PredicateType: predicateOpenVEX}
	cosign := repository.Attestation{ArtifactType: artifactCosignSig}
	notary := repository.Attestation{ArtifactType: artifactNotation}
	atts := []repository.Attestation{spdx, cyclonedx, openvex, cosign, notary}

	assert.Len(t, FilterByType(atts, nil), 5, "an empty filter keeps everything")
	assert.Len(t, FilterByType(atts, []string{"sbom"}), 2, "the sbom group alias catches spdx and cyclonedx")
	assert.Len(t, FilterByType(atts, []string{"signature"}), 2, "the signature group alias catches cosign and notation")

	got := FilterByType(atts, []string{"cyclonedx"})
	require.Len(t, got, 1)
	assert.Equal(t, repository.MediaTypeCycloneDXJSON, got[0].Type())

	assert.Len(t, FilterByType(atts, []string{"SPDX", "openvex"}), 2, "case-insensitive, union across filters")
}
