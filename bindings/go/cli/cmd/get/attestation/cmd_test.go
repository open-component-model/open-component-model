package attestation

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliattestation "ocm.software/open-component-model/bindings/go/cli/internal/attestation"
	"ocm.software/open-component-model/bindings/go/repository"
)

// OpenVEX media type, inlined because the unopinionated model defines no type constants.
const mediaTypeOpenVEX = "application/openvex+json"

func findOrigin(t *testing.T, origins []jsonOrigin, name string) jsonOrigin {
	t.Helper()
	for _, o := range origins {
		if o.Origin == name {
			return o
		}
	}
	require.Failf(t, "origin not found", "no origin %q in %v", name, origins)
	return jsonOrigin{}
}

// TestRenderTree_RawStructure pins the raw provenance nesting: component -> origin ->
// source -> {envelope media type -> predicate} | raw media type leaf, with the friendly
// TYPE tag and the platform suffix, and no faceted header.
func TestRenderTree_RawStructure(t *testing.T) {
	const spdxDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	groups := []cliattestation.Group{
		{Label: "name=image", Attestations: []repository.Attestation{
			// BuildKit SPDX in-toto statement, platform-scoped: envelope node + predicate leaf.
			{
				Source:        repository.AttestationSourceOCIIndex,
				PredicateType: repository.PredicateTypeSPDX,
				MediaType:     mediaInTotoStatement,
				Platform:      repository.Platform{OS: "linux", Architecture: "amd64"},
				ContentDigest: spdxDigest,
				ID:            "i1",
			},
			// Cosign-signed DSSE bundle: envelope node (its media type) + predicate leaf.
			{
				Source:        repository.AttestationSourceOCIReferrer,
				ArtifactType:  artifactSigstoreBundle,
				MediaType:     artifactSigstoreBundle,
				PredicateType: predicateCosignSig,
				ContentDigest: "sha256:sig",
				ID:            "r1",
			},
			// Bare CycloneDX document: a leaf named by its media type.
			{
				Source:        repository.AttestationSourceOCIReferrer,
				ArtifactType:  repository.MediaTypeCycloneDXJSON,
				MediaType:     repository.MediaTypeCycloneDXJSON,
				PredicateType: repository.PredicateTypeCycloneDX,
				ContentDigest: "sha256:cdx",
				ID:            "r2",
			},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "tree", component{Ref: "comp:1.0.0", Groups: groups}, false))
	out := buf.String()

	assert.Contains(t, out, "comp:1.0.0", "the component:version is the tree root")
	assert.Contains(t, out, "name=image", "the resource is an origin level")
	assert.Contains(t, out, "oci-index")
	assert.Contains(t, out, "oci-referrer")
	assert.Contains(t, out, mediaInTotoStatement, "the in-toto envelope is a node named by its media type")
	assert.Contains(t, out, repository.PredicateTypeSPDX, "the predicate type is a leaf under the envelope")
	assert.Contains(t, out, "(linux/amd64)", "the platform is a suffix on the predicate leaf")
	assert.Contains(t, out, artifactSigstoreBundle, "the DSSE bundle is a node named by its media type")
	assert.Contains(t, out, predicateCosignSig, "the cosign predicate is a leaf under the bundle")
	assert.Contains(t, out, repository.MediaTypeCycloneDXJSON, "the bare CycloneDX is a leaf named by its media type")
	assert.Contains(t, out, "sbom", "the SPDX predicate carries the friendly sbom tag")
	assert.Contains(t, out, "signature", "the cosign predicate carries the friendly signature tag")
	assert.Contains(t, out, spdxDigest, "the tree shows the full content digest, like oras discover")

	assert.NotContains(t, out, "NESTING", "the pure tree has no header")
	assert.NotContains(t, out, "CATEGORY", "the old faceted columns are gone")
	assert.NotContains(t, out, "ENVELOPE", "the envelope is a node, not a column")
}

// TestRenderTree_EmptyPrintsLine pins that an empty result prints a single explanatory
// line instead of a bare header (there is no header in the pure tree).
func TestRenderTree_EmptyPrintsLine(t *testing.T) {
	for _, format := range []string{"tree"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, renderAttestations(t.Context(), &buf, format, component{Ref: "example.com/app:1.0.0"}, false))
			assert.Equal(t, "no attestations found for example.com/app:1.0.0\n", buf.String())
		})
	}
}

// TestRenderAttestations_JSONNested pins that json mirrors the tree: nested origins ->
// sources -> documents, where a bare document carries its raw fields and the friendly type
// and an enveloped document carries its predicates.
func TestRenderAttestations_JSONNested(t *testing.T) {
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ArtifactType: mediaTypeOpenVEX, CreatedAt: "2026-11-24T15:04:40Z", ContentDigest: "sha256:vex", ID: "c1"},
		}},
		{Label: "name=image", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIIndex, PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement, Platform: repository.Platform{OS: "linux", Architecture: "amd64"}, ContentDigest: "sha256:spdx", ID: "i1"},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "json", component{Ref: "ref", Groups: groups}, false))

	var out jsonTree
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "ref", out.Component)
	require.Len(t, out.Origins, 2)

	comp := findOrigin(t, out.Origins, "(component-level)")
	require.Len(t, comp.Sources, 1)
	assert.Equal(t, "oci-referrer", comp.Sources[0].Source)
	require.Len(t, comp.Sources[0].Documents, 1)
	bare := comp.Sources[0].Documents[0]
	assert.False(t, bare.Enveloped, "a bare document is not enveloped")
	assert.Equal(t, "vex", bare.Type, "the friendly type travels in json")
	assert.Equal(t, mediaTypeOpenVEX, bare.MediaType)
	assert.Equal(t, "2026-11-24T15:04:40Z", bare.CreatedAt, "the creation timestamp travels in json")
	assert.Empty(t, bare.Predicates, "a bare document has no predicates")

	res := findOrigin(t, out.Origins, "name=image")
	require.Len(t, res.Sources, 1)
	assert.Equal(t, "oci-index", res.Sources[0].Source)
	require.Len(t, res.Sources[0].Documents, 1)
	env := res.Sources[0].Documents[0]
	assert.True(t, env.Enveloped, "an in-toto statement is enveloped")
	assert.Equal(t, mediaInTotoStatement, env.MediaType, "the envelope is named by its media type")
	require.Len(t, env.Predicates, 1)
	assert.Equal(t, repository.PredicateTypeSPDX, env.Predicates[0].PredicateType)
	assert.Equal(t, "sbom", env.Predicates[0].Type)
	assert.Equal(t, "linux/amd64", env.Predicates[0].Platform)
}

func TestRenderTable_RawColumnsAndFullDigest(t *testing.T) {
	const full = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	groups := []cliattestation.Group{
		{Label: "name=app", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement, ID: full, ContentDigest: full},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "table", component{Ref: "ref", Groups: groups}, false))
	out := buf.String()
	assert.Contains(t, out, "MEDIA-TYPE", "the table carries the raw media type column")
	assert.Contains(t, out, "PREDICATE-TYPE", "the table carries the raw predicate type column")
	assert.Contains(t, out, "name=app", "the table carries the resource origin")
	assert.Contains(t, out, mediaInTotoStatement, "the raw media type is shown")
	assert.Contains(t, out, repository.PredicateTypeSPDX, "the raw predicate type is shown")
	assert.Contains(t, out, "sbom", "the friendly type tag is kept as a column")
	assert.Contains(t, out, full, "the table shows the full digest, not a truncation")
	assert.NotContains(t, out, "CATEGORY", "the CATEGORY column is renamed TYPE")
	assert.NotContains(t, out, "ENVELOPE", "the ENVELOPE column is gone")
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

func TestRenderAttestations_RecursiveAddsComponentColumn(t *testing.T) {
	root := component{
		Ref: "root:1.0.0",
		Groups: []cliattestation.Group{
			{Label: "", Attestations: []repository.Attestation{
				{Source: repository.AttestationSourceOCIReferrer, ArtifactType: "sbom", MediaType: repository.MediaTypeCycloneDXJSON, ID: "sha256:root"},
			}},
		},
		Children: []component{
			{Ref: "child:2.0.0", Groups: []cliattestation.Group{
				{Label: "", Attestations: []repository.Attestation{
					{Source: repository.AttestationSourceOCIReferrer, ArtifactType: "vex", MediaType: mediaTypeOpenVEX, ID: "sha256:child"},
				}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "table", root, false))
	out := buf.String()
	assert.Contains(t, out, "COMPONENT", "more than one component adds the COMPONENT column")
	assert.Contains(t, out, "root:1.0.0", "the root component is shown")
	assert.Contains(t, out, "child:2.0.0", "the referenced component's attestations are listed too")
	assert.Contains(t, out, "(component-level)", "with a COMPONENT column the component group is labelled (component-level)")
}

func TestRenderTree_ComponentLevelLabelAndRecursion(t *testing.T) {
	root := component{
		Ref: "root:1.0.0",
		Groups: []cliattestation.Group{
			{Label: "", Attestations: []repository.Attestation{
				{Source: repository.AttestationSourceOCIReferrer, ArtifactType: "sbom", MediaType: repository.MediaTypeCycloneDXJSON, ID: "sha256:root"},
			}},
		},
		Children: []component{
			{Ref: "child:2.0.0", Groups: []cliattestation.Group{
				{Label: "", Attestations: []repository.Attestation{
					{Source: repository.AttestationSourceOCIReferrer, ArtifactType: "vex", MediaType: mediaTypeOpenVEX, ID: "sha256:child"},
				}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "tree", root, false))
	out := buf.String()
	assert.Contains(t, out, "root:1.0.0", "the root component:version is the tree root")
	assert.Contains(t, out, "child:2.0.0", "the referenced component nests under the root")
	assert.Contains(t, out, "(component-level)", "the component group is (component-level), the name is the parent node")
}

func TestColorGate(t *testing.T) {
	// painter is a pure pass-through when off, so --no-color, a pipe and json/yaml stay plain.
	off := painter{on: false}
	assert.Equal(t, "sbom", off.paint("sbom", text.FgGreen))
	assert.Empty(t, off.paint("", text.FgGreen))

	// When on, a non-empty cell is wrapped in ANSI (go-pretty colours forced on for the test,
	// since it otherwise suppresses colour on a non-terminal).
	text.EnableColors()
	t.Cleanup(text.DisableColors)
	on := painter{on: true}
	assert.Contains(t, on.paint("sbom", text.FgGreen), "\x1b[", "an enabled painter emits ANSI")
	assert.Empty(t, on.paint("", text.FgGreen), "an empty cell is never painted")

	// colorEnabled gates on the writer and the flags: a buffer is never a terminal.
	assert.False(t, colorEnabled(&bytes.Buffer{}, false), "a non-terminal writer never gets colour")
	assert.False(t, colorEnabled(&bytes.Buffer{}, true), "--no-color disables colour")
	t.Setenv("NO_COLOR", "1")
	assert.False(t, colorEnabled(&bytes.Buffer{}, false), "NO_COLOR disables colour")
}

// TestContentDigest_DuplicatesTagged pins the duplicate UX in the pure tree: byte-identical
// documents attached in different places share one letter tag, a unique document carries none.
func TestContentDigest_DuplicatesTagged(t *testing.T) {
	const dup = "sha256:dup0000000000000000000000000000000000000000000000000000000000"
	const uniq = "sha256:uniq000000000000000000000000000000000000000000000000000000000"
	plainVEX := func(digest, id string) repository.Attestation {
		return repository.Attestation{
			Source:        repository.AttestationSourceOCIReferrer,
			ArtifactType:  mediaTypeOpenVEX,
			MediaType:     mediaTypeOpenVEX,
			ContentDigest: digest,
			ID:            id,
		}
	}
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{plainVEX(dup, "c1")}},
		{Label: "name=image", Attestations: []repository.Attestation{
			plainVEX(dup, "r1"),
			{
				Source:        repository.AttestationSourceOCIReferrer,
				ArtifactType:  artifactSigstoreBundle,
				MediaType:     artifactSigstoreBundle,
				PredicateType: predicateOpenVEX,
				ContentDigest: uniq,
				ID:            "r2",
			},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, "tree", component{Ref: "ref", Groups: groups}, false))
	out := buf.String()

	assert.Equal(t, 2, strings.Count(out, "[a] "+dup), "both copies of the duplicate carry the same letter tag")
	assert.Contains(t, out, uniq, "the unique document is shown")
	assert.NotContains(t, out, "[a] "+uniq, "the unique document carries no duplicate tag")
	assert.NotContains(t, out, "[b]", "only one duplicate group exists")
}

// TestRenderAttestations_FailedOriginNamesImage pins that an origin whose discovery failed
// stays visible, names the image reference that failed, and reports the reason across tree,
// table and json.
func TestRenderAttestations_FailedOriginNamesImage(t *testing.T) {
	const ref = "ghcr.io/acme/img@sha256:bad"
	groups := []cliattestation.Group{
		{Label: "name=image", Reference: ref, Err: errors.New("resolving image reference failed (not found)")},
	}

	var tree bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &tree, "tree", component{Ref: "ref", Groups: groups}, false))
	assert.Contains(t, tree.String(), "attestation discovery failed for "+ref, "the tree names the failing image reference")
	assert.Contains(t, tree.String(), "not found", "the concise reason is shown")

	var tbl bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &tbl, "table", component{Ref: "ref", Groups: groups}, false))
	assert.Contains(t, tbl.String(), "name=image", "the failed resource stays visible in the table")
	assert.Contains(t, tbl.String(), "not found", "the table shows the reason")

	var js bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &js, "json", component{Ref: "ref", Groups: groups}, false))
	var out jsonTree
	require.NoError(t, json.Unmarshal(js.Bytes(), &out))
	o := findOrigin(t, out.Origins, "name=image")
	assert.Equal(t, ref, o.Reference, "json carries the failing image reference")
	assert.Contains(t, o.Error, "not found", "json carries the error")
}

// TestFilterTree_ContentDigest pins that the content-digest filter keeps every copy of the
// requested document across origins and drops everything else.
func TestFilterTree_ContentDigest(t *testing.T) {
	const want = "sha256:wanted00000000000000000000000000000000000000000000000000000000"
	mk := func(src repository.AttestationSource, digest, id string) repository.Attestation {
		return repository.Attestation{Source: src, ArtifactType: mediaTypeOpenVEX, MediaType: mediaTypeOpenVEX, ContentDigest: digest, ID: id}
	}
	root := component{
		Ref: "ref",
		Groups: []cliattestation.Group{
			{Label: "", Attestations: []repository.Attestation{
				mk(repository.AttestationSourceOCIReferrer, want, "c1"),
				mk(repository.AttestationSourceOCIReferrer, "sha256:other", "c2"),
			}},
			{Label: "name=img", Attestations: []repository.Attestation{
				mk(repository.AttestationSourceOCMLocal, want, "r1"),
			}},
		},
	}
	filtered, ok := filterTree(root, nil, want)
	require.True(t, ok)
	var got []string
	for _, g := range filtered.Groups {
		for _, a := range g.Attestations {
			got = append(got, a.ContentDigest)
		}
	}
	assert.Equal(t, []string{want, want}, got, "both copies of the wanted digest kept, the other dropped")
}
