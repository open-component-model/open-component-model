// sbom_test disclaimer: These tests were partially generated.
package sbom_test

import (
	"bytes"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/sbom"
)

var stamp = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

// taken from SPDX doc, this is a minimal, but valid SBOM doc to test merging.
func spdxDoc(name string) []byte {
	return []byte(`{
  "spdxVersion": "SPDX-2.3",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "` + name + `",
  "documentNamespace": "https://example.com/` + name + `",
  "creationInfo": {"created": "2026-01-01T00:00:00Z", "creators": ["Tool: test"]},
  "packages": [{
    "SPDXID": "SPDXRef-Package-` + name + `",
    "name": "pkg-` + name + `",
    "versionInfo": "1.0.0",
    "downloadLocation": "NOASSERTION"
  }],
  "relationships": [{
    "spdxElementId": "SPDXRef-DOCUMENT",
    "relatedSpdxElement": "SPDXRef-Package-` + name + `",
    "relationshipType": "DESCRIBES"
  }]
}`)
}

// cdxDoc is a minimal CycloneDX 1.6 document, so a mixed-format set can be combined.
func cdxDoc(name string) []byte {
	return []byte(`{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": {"component": {"type": "application", "name": "` + name + `", "bom-ref": "root-` + name + `"}},
  "components": [{"type": "library", "name": "pkg-` + name + `", "version": "2.0.0", "bom-ref": "ref-` + name + `"}]
}`)
}

func resource() *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta:    descriptor.ObjectMeta{Name: "image", Version: "1.0.0"},
			ExtraIdentity: runtime.Identity{"os": "linux", "architecture": "amd64"},
		},
		Type: "ociImage",
	}
}

func documents(names ...string) []sbom.Document {
	out := make([]sbom.Document, 0, len(names))
	for _, name := range names {
		out = append(out, sbom.Document{Resource: resource(), Name: name, Data: spdxDoc(name)})
	}
	return out
}

// render combines and serialises in one step, which is how every caller uses this.
func render(t *testing.T, docs []sbom.Document, format string) []byte {
	t.Helper()
	combined, err := sbom.Combine(docs, resource(), stamp)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, sbom.Write(combined, format, &out))
	return out.Bytes()
}

type spdxOut struct {
	SPDXVersion string `json:"spdxVersion"`
	Name        string `json:"name"`
	Packages    []struct {
		SPDXID string `json:"SPDXID"`
		Name   string `json:"name"`
	} `json:"packages"`
	Relationships []struct {
		RelatedSPDXElement string `json:"relatedSpdxElement"`
		RelationshipType   string `json:"relationshipType"`
	} `json:"relationships"`
}

func (s spdxOut) names() []string {
	out := make([]string, 0, len(s.Packages))
	for _, p := range s.Packages {
		out = append(out, p.Name)
	}
	return out
}

func (s spdxOut) roots() []string {
	var out []string
	for _, r := range s.Relationships {
		if r.RelationshipType == "DESCRIBES" {
			out = append(out, r.RelatedSPDXElement)
		}
	}
	return out
}

func parseSPDX(t *testing.T, raw []byte) spdxOut {
	t.Helper()
	var doc spdxOut
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func TestCombine(t *testing.T) {
	t.Run("absorbs every source document", func(t *testing.T) {
		doc := parseSPDX(t, render(t, documents("a", "b", "c"), sbom.FormatSPDX))
		assert.Subset(t, doc.names(), []string{"pkg-a", "pkg-b", "pkg-c"})
	})

	t.Run("roots the result at the resource", func(t *testing.T) {
		doc := parseSPDX(t, render(t, documents("a", "b"), sbom.FormatSPDX))
		require.Len(t, doc.roots(), 1, "one root however many sources went in")
		assert.Contains(t, doc.roots()[0], "ocm-resource")
		assert.Equal(t, "image", doc.Name)
	})

	t.Run("produces spdx ids the spec allows", func(t *testing.T) {
		doc := parseSPDX(t, render(t, documents("a"), sbom.FormatSPDX))
		valid := regexp.MustCompile(`^SPDXRef-[a-zA-Z0-9.-]+$`)
		require.NotEmpty(t, doc.Packages)
		for _, pkg := range doc.Packages {
			assert.Regexp(t, valid, pkg.SPDXID)
		}
		for _, root := range doc.roots() {
			assert.Regexp(t, valid, root)
		}
	})

	t.Run("serialises to cyclonedx", func(t *testing.T) {
		raw := render(t, documents("a", "b"), sbom.FormatCycloneDX)
		var doc struct {
			BOMFormat   string `json:"bomFormat"`
			SpecVersion string `json:"specVersion"`
		}
		require.NoError(t, json.Unmarshal(raw, &doc))
		assert.Equal(t, "CycloneDX", doc.BOMFormat)
		assert.NotEmpty(t, doc.SpecVersion)
	})

	t.Run("combines documents of different formats", func(t *testing.T) {
		// The point of going through protobom: a resource can carry an SPDX document
		// from one producer and a CycloneDX one from another, and both have to land in
		// the same output whichever format is asked for.
		mixed := []sbom.Document{
			{Resource: resource(), Name: "from-spdx", Data: spdxDoc("fromspdx")},
			{Resource: resource(), Name: "from-cdx", Data: cdxDoc("fromcdx")},
		}

		t.Run("into spdx", func(t *testing.T) {
			doc := parseSPDX(t, render(t, mixed, sbom.FormatSPDX))
			assert.Subset(t, doc.names(), []string{"pkg-fromspdx", "pkg-fromcdx"})
			require.Len(t, doc.roots(), 1)
		})

		t.Run("into cyclonedx", func(t *testing.T) {
			var doc struct {
				BOMFormat  string `json:"bomFormat"`
				Components []struct {
					Name       string `json:"name"`
					Components []struct {
						Name string `json:"name"`
					} `json:"components"`
				} `json:"components"`
			}
			require.NoError(t, json.Unmarshal(render(t, mixed, sbom.FormatCycloneDX), &doc))
			assert.Equal(t, "CycloneDX", doc.BOMFormat)

			// CycloneDX nests components under their root, so both levels count.
			var names []string
			for _, c := range doc.Components {
				names = append(names, c.Name)
				for _, nested := range c.Components {
					names = append(names, nested.Name)
				}
			}
			assert.Subset(t, names, []string{"pkg-fromspdx", "pkg-fromcdx"})
		})
	})

	t.Run("does not consume its input", func(t *testing.T) {
		docs := documents("a", "b")
		before := append([]byte(nil), docs[1].Data...)

		first := render(t, docs, sbom.FormatSPDX)
		assert.Equal(t, before, docs[1].Data, "the raw bytes handed in must be untouched")

		second := render(t, docs, sbom.FormatSPDX)
		assert.Equal(t, parseSPDX(t, first).names(), parseSPDX(t, second).names(),
			"combining the same input twice gives the same packages")
	})

	t.Run("rejects a document it cannot parse", func(t *testing.T) {
		docs := documents("document name")
		docs[0].Data = []byte("this is not an sbom")

		_, err := sbom.Combine(docs, resource(), stamp)
		require.Error(t, err, "a document we found but cannot read is fatal, not skipped")
		assert.Contains(t, err.Error(), "document name", "the error names which document failed")
		assert.NotContains(t, err.Error(), "this is not an sbom", "error not found")
	})

	t.Run("rejects an empty set", func(t *testing.T) {
		_, err := sbom.Combine(nil, resource(), stamp)
		require.Error(t, err)
	})

	t.Run("rejects an unknown format on write", func(t *testing.T) {
		combined, err := sbom.Combine(documents("a"), resource(), stamp)
		require.NoError(t, err)
		require.Error(t, sbom.Write(combined, "spdx3", &bytes.Buffer{}))
	})
}
