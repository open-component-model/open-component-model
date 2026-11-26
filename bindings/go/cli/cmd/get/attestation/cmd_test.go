package attestation

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"log/slog"
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

// discardLogger is the logger passed to renderAttestations in tests: the tree and table
// views never log, and the content views only warn about skipped documents.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

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
				PredicateType: "https://sigstore.dev/cosign/sign/v1",
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
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "tree", component{Ref: "comp:1.0.0", Groups: groups}, false))
	out := buf.String()

	assert.Contains(t, out, "comp:1.0.0", "the component:version is the tree root")
	assert.Contains(t, out, "name=image", "the resource is an origin level")
	assert.Contains(t, out, "oci-index")
	assert.Contains(t, out, "oci-referrer")
	assert.Contains(t, out, mediaInTotoStatement, "the in-toto envelope is a node named by its media type")
	assert.Contains(t, out, repository.PredicateTypeSPDX, "the predicate type is a leaf under the envelope")
	assert.Contains(t, out, "(linux/amd64)", "the platform is a suffix on the predicate leaf")
	assert.Contains(t, out, artifactSigstoreBundle, "the DSSE bundle is a node named by its media type")
	assert.Contains(t, out, "https://sigstore.dev/cosign/sign/v1", "the cosign predicate is a leaf under the bundle")
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
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "tree", component{Ref: "example.com/app:1.0.0"}, false))
	assert.Equal(t, "no attestations found for example.com/app:1.0.0\n", buf.String())
}

// TestRenderContent_JSON pins that -o json emits the decoded inner documents: a bare
// document unchanged and an in-toto statement reduced to its predicate, as a JSON array.
func TestRenderContent_JSON(t *testing.T) {
	vex := `{"@context":"https://openvex.dev/ns/v0.2.0","statements":[]}`
	statement := `{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://spdx.dev/Document","predicate":{"name":"the-sbom"}}`
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ContentDigest: "sha256:vex", ID: "c1", Data: []byte(vex)},
		}},
		{Label: "name=image", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIIndex, PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement, ContentDigest: "sha256:spdx", ID: "i1", Data: []byte(statement)},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "json", component{Ref: "ref", Groups: groups}, false))

	var docs []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &docs), "multiple documents are emitted as a json array")
	require.Len(t, docs, 2)

	var sawVEX, sawPredicate bool
	for _, d := range docs {
		if d["@context"] == "https://openvex.dev/ns/v0.2.0" {
			sawVEX = true
		}
		if d["name"] == "the-sbom" {
			sawPredicate = true
		}
	}
	assert.True(t, sawVEX, "the bare VEX document is emitted unchanged")
	assert.True(t, sawPredicate, "the in-toto statement is reduced to its predicate")
}

// TestRenderContent_JSONSingleIsBareObject pins that exactly one matching document is a bare
// object, not a one-element array (the shape --content-digest produces).
func TestRenderContent_JSONSingleIsBareObject(t *testing.T) {
	vex := `{"@context":"https://openvex.dev/ns/v0.2.0"}`
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ContentDigest: "sha256:vex", ID: "c1", Data: []byte(vex)},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "json", component{Ref: "ref", Groups: groups}, false))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc), "a single document is a bare object")
	assert.Equal(t, "https://openvex.dev/ns/v0.2.0", doc["@context"])
}

// TestRenderContent_YAMLStream pins that -o yaml writes a document stream separated by the
// yaml document marker.
func TestRenderContent_YAMLStream(t *testing.T) {
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ContentDigest: "sha256:a", ID: "c1", Data: []byte(`{"doc":"a"}`)},
			{Source: repository.AttestationSourceOCIReferrer, MediaType: mediaTypeOpenVEX, ContentDigest: "sha256:b", ID: "c2", Data: []byte(`{"doc":"b"}`)},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "yaml", component{Ref: "ref", Groups: groups}, false))
	out := buf.String()
	assert.Contains(t, out, "doc: a")
	assert.Contains(t, out, "doc: b")
	assert.Contains(t, out, "---", "multiple documents are separated by a yaml document marker")
}

// TestRenderContent_SkipsUndecodable pins that a document with no fetched bytes or with
// non-JSON bytes is skipped rather than corrupting the machine-readable output.
func TestRenderContent_SkipsUndecodable(t *testing.T) {
	groups := []cliattestation.Group{
		{Label: "", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, ContentDigest: "sha256:ok", ID: "c1", Data: []byte(`{"doc":"ok"}`)},
			{Source: repository.AttestationSourceOCIReferrer, ContentDigest: "sha256:bin", ID: "c2", Data: []byte("\x00binary-signature")},
			{Source: repository.AttestationSourceOCIReferrer, ContentDigest: "sha256:nil", ID: "c3"}, // no Data fetched
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "json", component{Ref: "ref", Groups: groups}, false))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc), "only the one decodable document remains, as a bare object")
	assert.Equal(t, "ok", doc["doc"])
}

func TestRenderTable_RawColumnsAndFullDigest(t *testing.T) {
	const full = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	groups := []cliattestation.Group{
		{Label: "name=app", Attestations: []repository.Attestation{
			{Source: repository.AttestationSourceOCIReferrer, PredicateType: repository.PredicateTypeSPDX, MediaType: mediaInTotoStatement, ID: full, ContentDigest: full},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "table", component{Ref: "ref", Groups: groups}, false))
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
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "table", root, false))
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
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "tree", root, false))
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
				PredicateType: "https://openvex.dev/ns",
				ContentDigest: uniq,
				ID:            "r2",
			},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &buf, discardLogger(), "tree", component{Ref: "ref", Groups: groups}, false))
	out := buf.String()

	assert.Equal(t, 2, strings.Count(out, "[a] "+dup), "both copies of the duplicate carry the same letter tag")
	assert.Contains(t, out, uniq, "the unique document is shown")
	assert.NotContains(t, out, "[a] "+uniq, "the unique document carries no duplicate tag")
	assert.NotContains(t, out, "[b]", "only one duplicate group exists")
}

// TestRenderAttestations_FailedOriginNamesImage pins that an origin whose discovery failed
// stays visible across the tree and table views, naming the image reference and the reason.
func TestRenderAttestations_FailedOriginNamesImage(t *testing.T) {
	const ref = "ghcr.io/acme/img@sha256:bad"
	groups := []cliattestation.Group{
		{Label: "name=image", Reference: ref, Err: errors.New("resolving image reference failed (not found)")},
	}

	var tree bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &tree, discardLogger(), "tree", component{Ref: "ref", Groups: groups}, false))
	assert.Contains(t, tree.String(), "attestation discovery failed for "+ref, "the tree names the failing image reference")
	assert.Contains(t, tree.String(), "not found", "the concise reason is shown")

	var tbl bytes.Buffer
	require.NoError(t, renderAttestations(t.Context(), &tbl, discardLogger(), "table", component{Ref: "ref", Groups: groups}, false))
	assert.Contains(t, tbl.String(), "name=image", "the failed resource stays visible in the table")
	assert.Contains(t, tbl.String(), "not found", "the table shows the reason")
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
