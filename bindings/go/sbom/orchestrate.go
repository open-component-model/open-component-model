package sbom

import (
	"bytes"
	"fmt"
	"strings"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

// Node is one component version in the orchestration tree. It carries the
// CycloneDX SBOMs discovered for its own resources plus its referenced child
// component versions (populated only in recursive mode).
type Node struct {
	// Component is the OCM component name (e.g. ocm.software/test-sbom).
	Component string
	// Version is the component version.
	Version string
	// Resources are the discovered, CycloneDX-normalized SBOMs of this
	// component version's resources.
	Resources []ResourceSBOM
	// Children are the referenced child component versions.
	Children []*Node
}

// ResourceSBOM is a single resource's discovered SBOM, already normalized to
// CycloneDX (see NormalizeToCycloneDX).
type ResourceSBOM struct {
	// ResourceName is the OCM resource name the SBOM describes.
	ResourceName string
	// BOM is the resource's CycloneDX SBOM.
	BOM *cyclonedx.BOM
}

// Orchestrate assembles a single hierarchical CycloneDX 1.6 BOM for the given
// root component version. The result's metadata.component represents the root
// component; each resource SBOM and each child component version becomes a
// nested component, and a dependency graph links parents to their children.
//
// BOM-refs from every embedded SBOM are namespaced by their owning component
// version (component@version:ref) so that identical package refs from different
// sources do not collide in the merged document.
func Orchestrate(root *Node) (*cyclonedx.BOM, error) {
	if root == nil {
		return nil, fmt.Errorf("nil root node")
	}

	bom := cyclonedx.NewBOM()
	bom.SpecVersion = CycloneDXSpecVersion
	rootComponent := componentForNode(root)
	bom.Metadata = &cyclonedx.Metadata{Component: rootComponent}

	components := make([]cyclonedx.Component, 0)
	dependencies := make([]cyclonedx.Dependency, 0)

	// Recursively fold the tree into flat component + dependency lists, each
	// component nested under its parent via the Components field.
	rootRefs := foldNode(root, &components, &dependencies)
	dependencies = append(dependencies, cyclonedx.Dependency{
		Ref:          rootComponent.BOMRef,
		Dependencies: &rootRefs,
	})

	bom.Components = &components
	bom.Dependencies = &dependencies
	return bom, nil
}

// foldNode appends the components contributed by node (its resource SBOMs and
// child component versions) to components, records their dependency edges, and
// returns the list of bom-refs that node directly depends on.
func foldNode(node *Node, components *[]cyclonedx.Component, dependencies *[]cyclonedx.Dependency) []string {
	var directDeps []string

	// One nested component per resource SBOM, holding that SBOM's components.
	// A single OCM resource can yield multiple SBOMs (e.g. one attestation per
	// platform of a multi-arch image), so each gets a unique bom-ref suffix and
	// a unique nested-ref namespace to keep the document collision-free.
	seen := make(map[string]int)
	for _, res := range node.Resources {
		instance := seen[res.ResourceName]
		seen[res.ResourceName]++

		resComp := componentForResource(node, res, instance)
		if res.BOM != nil && res.BOM.Components != nil {
			nested := namespaceComponents(*res.BOM.Components, resComp.BOMRef)
			resComp.Components = &nested
		}
		*components = append(*components, resComp)
		directDeps = append(directDeps, resComp.BOMRef)
	}

	// One nested component per child component version, recursively folded.
	for _, child := range node.Children {
		childComp := componentForNode(child)
		childDeps := foldNode(child, components, dependencies)
		*dependencies = append(*dependencies, cyclonedx.Dependency{
			Ref:          childComp.BOMRef,
			Dependencies: &childDeps,
		})
		*components = append(*components, *childComp)
		directDeps = append(directDeps, childComp.BOMRef)
	}

	return directDeps
}

// componentForNode builds the CycloneDX component that represents an OCM
// component version.
func componentForNode(node *Node) *cyclonedx.Component {
	return &cyclonedx.Component{
		BOMRef:  componentVersionNamespace(node.Component, node.Version),
		Type:    cyclonedx.ComponentTypeApplication,
		Name:    node.Component,
		Version: node.Version,
	}
}

// componentForResource builds the CycloneDX component that represents a single
// OCM resource within a component version. instance disambiguates multiple SBOMs
// discovered for the same resource (e.g. per-platform image attestations).
func componentForResource(node *Node, res ResourceSBOM, instance int) cyclonedx.Component {
	ns := componentVersionNamespace(node.Component, node.Version)
	ref := ns + ":resource:" + res.ResourceName
	if instance > 0 {
		ref = fmt.Sprintf("%s#%d", ref, instance)
	}
	comp := cyclonedx.Component{
		BOMRef: ref,
		Type:   cyclonedx.ComponentTypeApplication,
		Name:   res.ResourceName,
	}
	// If the embedded SBOM has a metadata component, carry its version/purl.
	if res.BOM != nil && res.BOM.Metadata != nil && res.BOM.Metadata.Component != nil {
		comp.Version = res.BOM.Metadata.Component.Version
		comp.PackageURL = res.BOM.Metadata.Component.PackageURL
	}
	return comp
}

// namespaceComponents deep-namespaces the bom-refs of a slice of components (and
// their nested children) so refs from different sources cannot collide.
func namespaceComponents(in []cyclonedx.Component, ns string) []cyclonedx.Component {
	out := make([]cyclonedx.Component, len(in))
	for i, c := range in {
		if c.BOMRef != "" {
			c.BOMRef = ns + ":" + c.BOMRef
		}
		if c.Components != nil {
			nested := namespaceComponents(*c.Components, ns)
			c.Components = &nested
		}
		out[i] = c
	}
	return out
}

// componentVersionNamespace is the stable bom-ref prefix for a component version.
func componentVersionNamespace(component, version string) string {
	return sanitizeRef(component) + "@" + sanitizeRef(version)
}

// sanitizeRef removes characters that would make a bom-ref ambiguous.
func sanitizeRef(s string) string {
	return strings.NewReplacer(" ", "_", "\t", "_", "\n", "_").Replace(s)
}

// Encode serializes the orchestrating BOM as CycloneDX 1.6 JSON (indented).
func Encode(bom *cyclonedx.BOM) ([]byte, error) {
	var buf bytes.Buffer
	enc := cyclonedx.NewBOMEncoder(&buf, cyclonedx.BOMFileFormatJSON)
	enc.SetPretty(true)
	if err := enc.EncodeVersion(bom, CycloneDXSpecVersion); err != nil {
		return nil, fmt.Errorf("encoding orchestrating SBOM failed: %w", err)
	}
	return buf.Bytes(), nil
}
