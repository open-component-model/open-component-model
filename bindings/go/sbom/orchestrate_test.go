package sbom_test

import (
	"strings"
	"testing"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/sbom"
)

const cycloneDXInput = `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "metadata": { "component": { "type": "application", "name": "cli", "version": "0.11.0" } },
  "components": [
    { "bom-ref": "pkg:golang/example.com/foo@1.0.0", "type": "library", "name": "foo", "version": "1.0.0" }
  ]
}`

const spdxInput = `{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "sbom",
  "documentNamespace": "https://example.com/sbom",
  "creationInfo": { "created": "2024-01-01T00:00:00Z", "creators": ["Tool: test"] },
  "documentDescribes": ["SPDXRef-Package-root"],
  "packages": [
    {
      "name": "root",
      "SPDXID": "SPDXRef-Package-root",
      "versionInfo": "0.1.0",
      "downloadLocation": "NOASSERTION"
    },
    {
      "name": "foo",
      "SPDXID": "SPDXRef-Package-foo",
      "versionInfo": "1.0.0",
      "downloadLocation": "NOASSERTION"
    }
  ],
  "relationships": [
    {
      "spdxElementId": "SPDXRef-DOCUMENT",
      "relatedSpdxElement": "SPDXRef-Package-root",
      "relationshipType": "DESCRIBES"
    },
    {
      "spdxElementId": "SPDXRef-Package-root",
      "relatedSpdxElement": "SPDXRef-Package-foo",
      "relationshipType": "DEPENDS_ON"
    }
  ]
}`

func TestNormalizeToCycloneDX_CycloneDXPassthrough(t *testing.T) {
	bom, err := sbom.NormalizeToCycloneDX(strings.NewReader(cycloneDXInput), "application/vnd.cyclonedx+json")
	require.NoError(t, err)
	require.NotNil(t, bom)
	require.NotNil(t, bom.Components)
	names := componentNames(*bom.Components)
	assert.Contains(t, names, "foo")
}

func TestNormalizeToCycloneDX_SPDXConverted(t *testing.T) {
	bom, err := sbom.NormalizeToCycloneDX(strings.NewReader(spdxInput), "application/spdx+json")
	require.NoError(t, err)
	require.NotNil(t, bom)
	require.Equal(t, "CycloneDX", bom.BOMFormat)
	require.NotNil(t, bom.Components, "converted SPDX should yield CycloneDX components")
	names := componentNames(*bom.Components)
	assert.Contains(t, names, "foo", "the SPDX package should survive conversion")
}

func TestNormalizeToCycloneDX_SniffFallback(t *testing.T) {
	// No media-type hint: must sniff SPDX vs CycloneDX from content.
	bom, err := sbom.NormalizeToCycloneDX(strings.NewReader(cycloneDXInput), "")
	require.NoError(t, err)
	require.NotNil(t, bom.Components)
	assert.Contains(t, componentNames(*bom.Components), "foo")
}

func TestNormalizeToCycloneDX_Empty(t *testing.T) {
	_, err := sbom.NormalizeToCycloneDX(strings.NewReader("   "), "")
	require.Error(t, err)
}

func mustNormalize(t *testing.T, input, mediaType string) *cyclonedx.BOM {
	t.Helper()
	bom, err := sbom.NormalizeToCycloneDX(strings.NewReader(input), mediaType)
	require.NoError(t, err)
	return bom
}

func TestOrchestrate_SingleComponent(t *testing.T) {
	root := &sbom.Node{
		Component: "ocm.software/test-sbom",
		Version:   "0.1.0",
		Resources: []sbom.ResourceSBOM{
			{ResourceName: "cli", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
		},
	}

	bom, err := sbom.Orchestrate(root)
	require.NoError(t, err)
	require.NotNil(t, bom.Metadata)
	require.NotNil(t, bom.Metadata.Component)
	assert.Equal(t, "ocm.software/test-sbom", bom.Metadata.Component.Name)
	assert.Equal(t, "0.1.0", bom.Metadata.Component.Version)
	assert.Equal(t, cyclonedx.SpecVersion1_6, bom.SpecVersion)

	require.NotNil(t, bom.Components)
	// The resource "cli" should appear as a nested component.
	names := componentNames(*bom.Components)
	assert.Contains(t, names, "cli")

	// The resource component's nested package refs are namespaced by the CV.
	cli := findComponent(*bom.Components, "cli")
	require.NotNil(t, cli)
	require.NotNil(t, cli.Components)
	for _, nested := range *cli.Components {
		assert.True(t, strings.HasPrefix(nested.BOMRef, "ocm.software/test-sbom@0.1.0:"),
			"nested ref %q should be namespaced by the component version", nested.BOMRef)
	}
}

func TestOrchestrate_Recursive(t *testing.T) {
	child := &sbom.Node{
		Component: "ocm.software/test-sbom",
		Version:   "0.1.0",
		Resources: []sbom.ResourceSBOM{
			{ResourceName: "cli", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
		},
	}
	root := &sbom.Node{
		Component: "ocm.software/test-sbom-umbrella",
		Version:   "0.1.0",
		Resources: []sbom.ResourceSBOM{
			{ResourceName: "test-binary", BOM: mustNormalize(t, spdxInput, "application/spdx+json")},
		},
		Children: []*sbom.Node{child},
	}

	bom, err := sbom.Orchestrate(root)
	require.NoError(t, err)
	require.NotNil(t, bom.Components)

	names := componentNames(*bom.Components)
	assert.Contains(t, names, "test-binary", "root resource present")
	assert.Contains(t, names, "cli", "child resource present")
	assert.Contains(t, names, "ocm.software/test-sbom", "child component version present")

	// Dependency graph must contain a root node linking to its direct children,
	// and the child CV node linking to its own resource.
	require.NotNil(t, bom.Dependencies)
	rootRef := "ocm.software/test-sbom-umbrella@0.1.0"
	childRef := "ocm.software/test-sbom@0.1.0"
	rootDeps := findDependency(*bom.Dependencies, rootRef)
	require.NotNil(t, rootDeps, "root dependency node must exist")
	assert.Contains(t, derefStrings(rootDeps.Dependencies), childRef,
		"root must depend on the child component version")

	childDeps := findDependency(*bom.Dependencies, childRef)
	require.NotNil(t, childDeps, "child CV dependency node must exist")
	assert.Contains(t, derefStrings(childDeps.Dependencies), childRef+":resource:cli")
}

func TestOrchestrate_NilRoot(t *testing.T) {
	_, err := sbom.Orchestrate(nil)
	require.Error(t, err)
}

func TestOrchestrate_DuplicateResourceSBOMsGetUniqueRefs(t *testing.T) {
	// One resource ("podinfo") yielding multiple SBOMs (e.g. per-platform image
	// attestations) must not produce colliding bom-refs.
	root := &sbom.Node{
		Component: "ocm.software/test-sbom",
		Version:   "0.1.0",
		Resources: []sbom.ResourceSBOM{
			{ResourceName: "podinfo", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
			{ResourceName: "podinfo", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
			{ResourceName: "podinfo", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
		},
	}
	bom, err := sbom.Orchestrate(root)
	require.NoError(t, err)
	require.NotNil(t, bom.Components)
	require.Len(t, *bom.Components, 3)

	refs := map[string]int{}
	for _, c := range *bom.Components {
		refs[c.BOMRef]++
	}
	for ref, n := range refs {
		assert.Equal(t, 1, n, "bom-ref %q must be unique across resource SBOMs", ref)
	}
	// The dependency graph must reference each unique ref exactly once.
	rootDeps := findDependency(*bom.Dependencies, "ocm.software/test-sbom@0.1.0")
	require.NotNil(t, rootDeps)
	assert.Len(t, derefStrings(rootDeps.Dependencies), 3)
}

func TestEncode_ProducesCycloneDXJSON(t *testing.T) {
	root := &sbom.Node{Component: "c", Version: "1", Resources: []sbom.ResourceSBOM{
		{ResourceName: "r", BOM: mustNormalize(t, cycloneDXInput, "application/vnd.cyclonedx+json")},
	}}
	bom, err := sbom.Orchestrate(root)
	require.NoError(t, err)
	data, err := sbom.Encode(bom)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"bomFormat": "CycloneDX"`)
	assert.Contains(t, string(data), `"specVersion": "1.6"`)
}

// --- helpers ---

func componentNames(cs []cyclonedx.Component) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func findComponent(cs []cyclonedx.Component, name string) *cyclonedx.Component {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}

func findDependency(ds []cyclonedx.Dependency, ref string) *cyclonedx.Dependency {
	for i := range ds {
		if ds[i].Ref == ref {
			return &ds[i]
		}
	}
	return nil
}

func derefStrings(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
}
