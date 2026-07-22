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
// Orchestrate assembles a single hierarchical CycloneDX 1.6 BOM for the given
// root component version. The result's metadata.component represents the root
// component; each resource SBOM and each child component version is represented
// by a structural component, and a dependency graph links parents to children.
//
// All components — the structural wrappers (component versions, resources) and
// every real package from the embedded SBOMs — are emitted as a FLAT top-level
// components list. The hierarchy is expressed purely through the dependencies
// graph, never through nested component.components sub-trees. This is what
// CycloneDX consumers such as Trivy expect: scanners walk the flat components
// list and will not descend into nested component sub-trees, so nesting hides the
// packages from vulnerability detection.
//
// BOM-refs from every embedded SBOM are namespaced by their owning resource
// component (component@version:resource:name[:ref]) so that identical package
// refs from different sources do not collide in the merged document.
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

	// Recursively fold the tree into flat component + dependency lists.
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
// child component versions) to components as a flat list, records their
// dependency edges, and returns the list of bom-refs that node directly depends
// on.
func foldNode(node *Node, components *[]cyclonedx.Component, dependencies *[]cyclonedx.Dependency) []string {
	var directDeps []string

	// One structural component per resource SBOM. A single OCM resource can yield
	// multiple SBOMs (e.g. one attestation per platform of a multi-arch image), so
	// each gets a unique bom-ref suffix and a unique ref namespace to keep the
	// document collision-free.
	seen := make(map[string]int)
	for _, res := range node.Resources {
		instance := seen[res.ResourceName]
		seen[res.ResourceName]++

		resComp := componentForResource(node, res, instance)
		*components = append(*components, resComp)
		directDeps = append(directDeps, resComp.BOMRef)

		// Emit the embedded SBOM's components FLAT (never nested) and wire them
		// under the resource component via the dependency graph.
		if res.BOM != nil {
			pkgRefs := appendEmbeddedSBOM(res.BOM, resComp.BOMRef, components, dependencies)
			*dependencies = append(*dependencies, cyclonedx.Dependency{
				Ref:          resComp.BOMRef,
				Dependencies: &pkgRefs,
			})
		}
	}

	// One structural component per child component version, recursively folded.
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

// appendEmbeddedSBOM flattens an embedded SBOM's components into the top-level
// components list (namespacing their bom-refs by ns), carries over the embedded
// SBOM's own dependency edges (also namespaced), and returns the direct package
// refs to attach under the owning resource component.
func appendEmbeddedSBOM(embedded *cyclonedx.BOM, ns string, components *[]cyclonedx.Component, dependencies *[]cyclonedx.Dependency) []string {
	if embedded.Components == nil {
		return nil
	}

	var directRefs []string
	flattenComponents(*embedded.Components, ns, components, &directRefs)

	// Preserve the embedded SBOM's internal dependency graph, namespaced.
	if embedded.Dependencies != nil {
		for _, dep := range *embedded.Dependencies {
			nsDep := cyclonedx.Dependency{Ref: namespaceRef(dep.Ref, ns)}
			if dep.Dependencies != nil {
				refs := make([]string, 0, len(*dep.Dependencies))
				for _, r := range *dep.Dependencies {
					refs = append(refs, namespaceRef(r, ns))
				}
				nsDep.Dependencies = &refs
			}
			*dependencies = append(*dependencies, nsDep)
		}
	}

	return directRefs
}

// flattenComponents appends every component (and, recursively, any nested
// component sub-trees the source SBOM happened to use) to out as a flat list with
// namespaced bom-refs. The refs of the top-level (direct) components are collected
// into directRefs. Nested sub-trees are flattened, not preserved, so no
// component.components remains in the output.
func flattenComponents(in []cyclonedx.Component, ns string, out *[]cyclonedx.Component, directRefs *[]string) {
	for _, c := range in {
		nested := c.Components
		c.Components = nil
		if c.BOMRef != "" {
			c.BOMRef = namespaceRef(c.BOMRef, ns)
		}
		*out = append(*out, c)
		if directRefs != nil && c.BOMRef != "" {
			*directRefs = append(*directRefs, c.BOMRef)
		}
		if nested != nil {
			// Deeper components are flattened too, but they are not "direct"
			// dependencies of the resource component.
			flattenComponents(*nested, ns, out, nil)
		}
	}
}

// namespaceRef prefixes a bom-ref with ns unless it is already prefixed.
func namespaceRef(ref, ns string) string {
	if ref == "" {
		return ref
	}
	return ns + ":" + ref
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
