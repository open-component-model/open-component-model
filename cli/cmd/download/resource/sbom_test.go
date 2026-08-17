// resource_test disclaimer: some parts of these tests have been generated.
package resource_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artefactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artefactref/v1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

const (
	componentName    = "ocm.software/test-sbom"
	componentVersion = "1.0.0"
	targetName       = "image"
)

type resourceSpec struct {
	name          string
	resourceType  string
	extraIdentity runtime.Identity
	describes     runtime.Identity
	content       string
}

func setupComponent(t *testing.T, specs ...resourceSpec) string {
	t.Helper()
	r := require.New(t)

	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
		},
	}

	for _, spec := range specs {
		content := []byte(spec.content)
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta:    descriptor.ObjectMeta{Name: spec.name, Version: componentVersion},
				ExtraIdentity: spec.extraIdentity,
			},
			Type:     spec.resourceType,
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(content).String(),
				MediaType:      "application/json",
			},
		}

		if spec.describes != nil {
			value, err := json.Marshal(artefactref.Reference{Identity: spec.describes})
			r.NoError(err)
			resource.Labels = []descriptor.Label{{
				Name:    artefactref.LabelName,
				Version: artefactref.Version,
				Value:   value,
			}}
		}

		added, err := repo.AddLocalResource(t.Context(), componentName, componentVersion,
			resource, inmemory.New(bytes.NewReader(content)))
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *added)
	}

	r.NoError(repo.AddComponentVersion(t.Context(), desc))

	ref := &compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  componentName,
		Version:    componentVersion,
	}
	return ref.String()
}

func target() resourceSpec {
	return resourceSpec{name: targetName, resourceType: "blob", content: "the artifact itself"}
}

func describesTarget() runtime.Identity {
	return runtime.Identity{"name": targetName, "version": componentVersion}
}

// spdx is a minimal but complete SPDX 2.3 document.
func spdx(name string) string {
	return `{
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
}`
}

// combined is the parsed SPDX document the command produced.
type combined struct {
	SPDXVersion string `json:"spdxVersion"`
	Name        string `json:"name"`
	Packages    []struct {
		SPDXID string `json:"SPDXID"`
		Name   string `json:"name"`
	} `json:"packages"`
	Relationships []struct {
		SPDXElementID      string `json:"spdxElementId"`
		RelatedSPDXElement string `json:"relatedSpdxElement"`
		RelationshipType   string `json:"relationshipType"`
	} `json:"relationships"`
}

func (c combined) packageNames() []string {
	names := make([]string, 0, len(c.Packages))
	for _, p := range c.Packages {
		names = append(names, p.Name)
	}
	return names
}

// roots returns what the document declares itself to describe.
func (c combined) roots() []string {
	var found []string
	for _, r := range c.Relationships {
		if r.RelationshipType == "DESCRIBES" {
			found = append(found, r.RelatedSPDXElement)
		}
	}
	return found
}

func parseCombined(t *testing.T, raw []byte) combined {
	t.Helper()
	var doc combined
	require.NoError(t, json.Unmarshal(raw, &doc), "the command must emit parseable json")
	return doc
}

// downloadSBOM runs the command with no --output and uses a scanner for fetching the content
// from stdout.
func downloadSBOM(t *testing.T, ref string, extra ...string) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	args := append([]string{"download", "resource", ref, "--identity", "name=" + targetName, "--sbom"}, extra...)
	_, err := test.OCM(t, test.WithArgs(args...), test.WithOutput(&out))
	return out.Bytes(), err
}

func TestDownloadResourceSBOM_ArtefactReference(t *testing.T) {
	t.Run("emits the sbom a sibling resource declares it describes", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "image-sbom",
				resourceType: "sbom",
				describes:    describesTarget(),
				content:      spdx("only"),
			},
		)

		raw, err := downloadSBOM(t, ref)
		require.NoError(t, err)

		doc := parseCombined(t, raw)
		assert.Equal(t, "SPDX-2.3", doc.SPDXVersion)
		assert.Contains(t, doc.packageNames(), "pkg-only")
	})

	t.Run("combines every describing sbom into one document", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"os": "linux", "architecture": "amd64"},
				describes:     describesTarget(),
				content:       spdx("amd64"),
			},
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"os": "linux", "architecture": "arm64"},
				describes:     describesTarget(),
				content:       spdx("arm64"),
			},
		)

		raw, err := downloadSBOM(t, ref)
		require.NoError(t, err)

		doc := parseCombined(t, raw)
		assert.Subset(t, doc.packageNames(), []string{"pkg-amd64", "pkg-arm64"},
			"both platforms' packages survive the merge; nothing is narrowed away")
	})

	t.Run("roots the combined document at the resource, not at a source document", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"architecture": "amd64"},
				describes:     describesTarget(),
				content:       spdx("amd64"),
			},
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"architecture": "arm64"},
				describes:     describesTarget(),
				content:       spdx("arm64"),
			},
		)

		raw, err := downloadSBOM(t, ref)
		require.NoError(t, err)

		doc := parseCombined(t, raw)
		// Exactly one root, whatever the number of sources. CycloneDX cannot be
		// serialised at all from a multi-root document, so this is load-bearing.
		require.Len(t, doc.roots(), 1)
		assert.Contains(t, doc.roots()[0], "ocm-resource")

		// SPDX constrains element ids, and an OCM identity is full of "=" and ",".
		valid := regexp.MustCompile(`^SPDXRef-[a-zA-Z0-9.-]+$`)
		for _, pkg := range doc.Packages {
			assert.Regexp(t, valid, pkg.SPDXID)
		}
	})

	t.Run("writes to a file when --output is given", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: spdx("only")},
		)
		output := filepath.Join(t.TempDir(), "combined.spdx.json")

		stdout, err := downloadSBOM(t, ref, "--output", output)
		require.NoError(t, err)
		assert.NotContains(t, string(stdout), "spdxVersion", "the document goes to the file, not stdout")

		written, err := os.ReadFile(output)
		require.NoError(t, err)
		assert.Contains(t, parseCombined(t, written).packageNames(), "pkg-only")
	})

	t.Run("emits cyclonedx when asked", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: spdx("only")},
		)

		raw, err := downloadSBOM(t, ref, "--sbom-format", "cyclonedx")
		require.NoError(t, err)

		var doc struct {
			BOMFormat   string `json:"bomFormat"`
			SpecVersion string `json:"specVersion"`
		}
		require.NoError(t, json.Unmarshal(raw, &doc))
		assert.Equal(t, "CycloneDX", doc.BOMFormat)
		assert.NotEmpty(t, doc.SpecVersion)
	})

	t.Run("ignores a describing resource that is not an sbom", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "image-signature",
				resourceType: "signature",
				describes:    describesTarget(),
				content:      "not an sbom",
			},
		)
		_, err := downloadSBOM(t, ref)
		require.Error(t, err, "the only describing resource is not an sbom, so the artifact is inspected instead")
	})

	t.Run("ignores a reference to a different resource", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "other-sbom",
				resourceType: "sbom",
				describes:    runtime.Identity{"name": "something-else", "version": componentVersion},
				content:      spdx("other"),
			},
		)
		_, err := downloadSBOM(t, ref)
		require.Error(t, err)
	})
}

func TestDownloadResourceSBOM_Errors(t *testing.T) {
	t.Run("reports when nothing describes the resource and it cannot be inspected", func(t *testing.T) {
		ref := setupComponent(t, target())

		_, err := downloadSBOM(t, ref)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no sbom found for resource")
	})

	t.Run("rejects a transformer alongside the flag", func(t *testing.T) {
		ref := setupComponent(t, target())
		_, err := downloadSBOM(t, ref, "--transformer", "anything")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[sbom transformer]")
	})

	t.Run("rejects an extraction policy alongside the flag", func(t *testing.T) {
		ref := setupComponent(t, target())
		_, err := downloadSBOM(t, ref, "--extraction-policy", "disable")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[sbom extraction-policy]")
	})
}

func TestDownloadResource_WithoutSBOMFlagIsUnaffected(t *testing.T) {
	ref := setupComponent(t,
		target(),
		resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: spdx("sbom")},
	)
	output := filepath.Join(t.TempDir(), "resource.bin")

	_, err := test.OCM(t, test.WithArgs(
		"download", "resource", ref, "--identity", "name="+targetName, "--output", output))
	require.NoError(t, err)

	written, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "the artifact itself", string(written), "the resource itself is downloaded, not its sbom")
}
