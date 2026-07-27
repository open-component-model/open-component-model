package runtime

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// LabelArtefactReference is the name of the label that links a resource to
	// another resource (an "artefact reference"), following the OCM spec
	// convention. An SBOM resource (type ResourceTypeSBOM) uses it to point at the
	// resource it describes; the SBOM-ness comes from the resource type, not the
	// label. Its value is an ArtefactReferenceLabelValue.
	//
	// Example (component constructor):
	//
	//	- name: cli-sbom
	//	  type: sbom
	//	  labels:
	//	    - name: ocm.software/artefactReference
	//	      version: v1
	//	      value:
	//	        identitySelector:
	//	          name: cli
	LabelArtefactReference = "ocm.software/artefactReference"

	// ResourceTypeSBOM is the OCM resource type used for resources that carry a
	// Software Bill of Materials (SBOM), e.g. in SPDX or CycloneDX format.
	ResourceTypeSBOM = "sbom"
)

// ArtefactReferenceLabelValue is the parsed value of the LabelArtefactReference
// label. It selects the resource that the labelled resource refers to.
type ArtefactReferenceLabelValue struct {
	// IdentitySelector is the (possibly partial) identity of the referenced
	// resource, as a flat map of identity attributes (e.g. {"name": "cli"} or
	// {"name": "podinfo", "architecture": "amd64"}). It is matched as a subset
	// against the full resource identity.
	IdentitySelector runtime.Identity `json:"identitySelector"`
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
// component version whose LabelArtefactReference label references the target identity.
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
		label := res.GetLabel(LabelArtefactReference)
		if label == nil {
			continue
		}

		var value ArtefactReferenceLabelValue
		if err := label.GetValue(&value); err != nil {
			return nil, fmt.Errorf("parsing %q label of sbom resource %q failed: %w", LabelArtefactReference, res.Name, err)
		}

		if runtime.IdentitySubset(value.IdentitySelector, target) {
			matches = append(matches, res)
		}
	}

	return matches, nil
}
