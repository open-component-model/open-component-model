package runtime

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// LabelSBOM is the name of the label that links an SBOM resource to the
	// resource(s) it describes. It is carried by a resource of type
	// ResourceTypeSBOM and its value is an SBOMLabelValue.
	//
	// Example (component constructor):
	//
	//	- name: cli-sbom
	//	  type: sbom
	//	  labels:
	//	    - name: ocm.software/sbom
	//	      version: v1
	//	      value:
	//	        references:
	//	          - resource:
	//	              name: cli
	LabelSBOM = "ocm.software/sbom"

	// ResourceTypeSBOM is the OCM resource type used for resources that carry a
	// Software Bill of Materials (SBOM), e.g. in SPDX or CycloneDX format.
	ResourceTypeSBOM = "sbom"
)

// SBOMLabelValue is the parsed value of the LabelSBOM label. It lists the
// resources that the SBOM resource describes.
type SBOMLabelValue struct {
	// References lists the resources described by the SBOM.
	References []SBOMReference `json:"references"`
}

// SBOMReference points to a single resource described by an SBOM resource via
// its element identity. The identity is typically partial (e.g. only the name),
// in which case it is matched as a subset against the full resource identity.
type SBOMReference struct {
	// Resource is the (possibly partial) identity of the described resource.
	Resource runtime.Identity `json:"resource"`
}

// GetLabel returns a pointer to the label with the given name, or nil if the
// element carries no such label. The returned pointer aliases the element's
// label slice; callers must not mutate it.
func (m *ElementMeta) GetLabel(name string) *Label {
	if m == nil {
		return nil
	}
	for i := range m.Labels {
		if m.Labels[i].Name == name {
			return &m.Labels[i]
		}
	}
	return nil
}

// FindSBOMResources returns the resources of type ResourceTypeSBOM in the given
// component version whose LabelSBOM label references the target identity.
//
// A reference matches when its (possibly partial) identity is a subset of the
// target identity (see runtime.IdentitySubset). This lets a label that only
// specifies {name: cli} match a resource that additionally carries version and
// extraIdentity attributes.
//
// Multiple matches are possible (e.g. one component may publish both an SPDX and
// a CycloneDX SBOM for the same resource); all matches are returned in
// descriptor order. If no SBOM resource references the target, an empty slice
// and a nil error are returned so callers can distinguish "no SBOM linked" from
// a malformed label (which yields an error).
//
// TODO: Add FindSBOMFromOCIReferrers for OCIImage resources that carry their
// SBOM as an in-index buildx attestation / OCI referrer (per ADR-0016). Callers
// can fall back to it when this function returns no matches. The OCI referrer
// plumbing lives under bindings/go/oci (spec/annotations, ctf/referrers).
func FindSBOMResources(desc *Descriptor, target runtime.Identity) ([]Resource, error) {
	if desc == nil {
		return nil, nil
	}

	var matches []Resource
	for _, res := range desc.Component.Resources {
		if res.Type != ResourceTypeSBOM {
			continue
		}
		label := res.GetLabel(LabelSBOM)
		if label == nil {
			continue
		}

		var value SBOMLabelValue
		if err := label.GetValue(&value); err != nil {
			return nil, fmt.Errorf("parsing %q label of sbom resource %q failed: %w", LabelSBOM, res.Name, err)
		}

		for _, ref := range value.References {
			if runtime.IdentitySubset(ref.Resource, target) {
				matches = append(matches, res)
				break
			}
		}
	}

	return matches, nil
}
