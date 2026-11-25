package attestation

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/repository"
)

// OpenVEX media type, inlined because the unopinionated model defines no type constants.
const mediaTypeOpenVEX = "application/openvex+json"

func TestRenderAttestations_JSONProjectsGroupsAndProvenance(t *testing.T) {
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ArtifactType: mediaTypeOpenVEX, CreatedAt: "2026-11-24T15:04:40Z", ID: "sha256:comp"},
		}},
		{Label: "name=app", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCMLocal, ArtifactType: "sbom", MediaType: repository.MediaTypeCycloneDXJSON, ID: "name=app-sbom"},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(context.Background(), &buf, "json", "ref", groups))

	var rows []row
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 2)

	// Sorted by resource: "(component)" sorts before "name=app".
	assert.Equal(t, "(component)", rows[0].Resource, "the component group is labelled, not blank")
	assert.Equal(t, "2026-11-24T15:04:40Z", rows[0].CreatedAt, "the creation timestamp travels in json")
	assert.Equal(t, "name=app", rows[1].Resource)
	assert.Equal(t, "sbom", rows[1].Type)
	assert.Equal(t, repository.MediaTypeCycloneDXJSON, rows[1].MediaType)
}

func TestRenderTable_ShowsResourceAndFullDigest(t *testing.T) {
	const full = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	groups := []cliattestation.Group{
		{Label: "name=app", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement, ID: full},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(context.Background(), &buf, "table", "ref", groups))
	out := buf.String()
	assert.Contains(t, out, "name=app", "the table carries the resource origin")
	assert.Contains(t, out, full, "the table shows the full digest, not a truncation")
}

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
			assert.Equal(t, c.want, typeAliases(c.att))
		})
	}
}

func TestFilterByType(t *testing.T) {
	spdx := repository.Attestation{PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement}
	cyclonedx := repository.Attestation{ArtifactType: repository.MediaTypeCycloneDXJSON, MediaType: repository.MediaTypeCycloneDXJSON}
	openvex := repository.Attestation{PredicateType: predicateOpenVEX}
	cosign := repository.Attestation{ArtifactType: artifactCosignSig}
	notary := repository.Attestation{ArtifactType: artifactNotation}
	atts := []repository.Attestation{spdx, cyclonedx, openvex, cosign, notary}

	assert.Len(t, filterByType(atts, nil), 5, "an empty filter keeps everything")
	assert.Len(t, filterByType(atts, []string{"sbom"}), 2, "the sbom group alias catches spdx and cyclonedx")
	assert.Len(t, filterByType(atts, []string{"signature"}), 2, "the signature group alias catches cosign and notation")

	got := filterByType(atts, []string{"cyclonedx"})
	require.Len(t, got, 1)
	assert.Equal(t, repository.MediaTypeCycloneDXJSON, got[0].Type())

	assert.Len(t, filterByType(atts, []string{"SPDX", "openvex"}), 2, "case-insensitive, union across filters")
}

func TestRenderTree_FacetedColumns(t *testing.T) {
	const bundleDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const indexDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	groups := []cliattestation.Group{
		{Label: "name=app", Attestations: []repository.Attestation{
			// cosign-signed OpenVEX: a DSSE bundle wrapping an openvex predicate.
			{
				Source:        repository.AttestationSourceOCIReferrer,
				ArtifactType:  artifactSigstoreBundle,
				PredicateType: predicateOpenVEX,
				Annotations:   map[string]string{annotationDSSEContent: "dsse-envelope"},
				CreatedAt:     "2026-11-24T15:04:41Z",
				ID:            bundleDigest,
			},
			// BuildKit SPDX in the image index, platform-scoped.
			{
				Source:        repository.AttestationSourceOCIIndex,
				PredicateType: repository.PredicateTypeSPDX,
				MediaType:     mediaInTotoStatement,
				Platform:      repository.Platform{OS: "linux", Architecture: "amd64"},
				ID:            indexDigest,
			},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(context.Background(), &buf, "widetree", "my-ref", groups))
	out := buf.String()

	assert.Contains(t, out, "my-ref", "the reference is the tree root")
	assert.Contains(t, out, "name=app", "the resource is a grouping level")
	assert.Contains(t, out, "oci-referrer")
	assert.Contains(t, out, "oci-index")
	// Facets are columns, not stacked nesting levels.
	assert.Contains(t, out, "dsse", "the DSSE envelope shows in the ENV column")
	assert.Contains(t, out, "in-toto", "the in-toto envelope shows in the ENV column")
	assert.Contains(t, out, "vex", "category of the signed openvex")
	assert.Contains(t, out, "openvex", "format of the signed openvex")
	assert.Contains(t, out, "sbom", "category of the index spdx")
	assert.Contains(t, out, "spdx", "format of the index spdx")
	assert.Contains(t, out, "linux/amd64", "platform shows in the PLATFORM column")
	assert.Contains(t, out, "2026-11-24T15:04:41Z", "the creation timestamp shows in widetree")
	assert.Contains(t, out, bundleDigest, "the tree shows the full digest, like oras discover")
	// The raw bundle artifactType and predicate URL are no longer nesting nodes; the
	// friendly category/format replace them in the tree (raw stays in json/yaml).
	assert.NotContains(t, out, artifactSigstoreBundle, "the raw bundle artifactType is not a tree node")
}
