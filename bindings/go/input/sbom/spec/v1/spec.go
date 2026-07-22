package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Version    = "v1"
	Type       = "SBoM"
	LegacyType = "sbom"
)

// SBOM describes an input that discovers the Software Bill of Materials (SBOM)
// attached to another resource's OCI image at construction time, merges the
// discovered SBOM(s) into a single CycloneDX document, and embeds it as a local
// blob in the component version.
//
// The subject resource is referenced by identity via Resource (typically just
// its name). The CLI resolves that reference to the subject's access before
// construction and fills Access, so the input method has a self-contained access
// to run discovery against. See the SBoM/v1 input method for details.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type SBOM struct {
	// +ocm:jsonschema-gen:enum=sbom/v1,SBoM/v1
	// +ocm:jsonschema-gen:enum:deprecated=sbom,SBoM
	Type runtime.Type `json:"type"`

	// Resource is the (possibly partial) identity of the resource whose SBOM
	// should be discovered and embedded, e.g. {"name": "podinfo"}. It is matched
	// as a subset against the sibling resources of the same component version.
	Resource runtime.Identity `json:"resource"`

	// Platform selects a single per-architecture SBOM from a multi-arch image
	// (e.g. "linux/amd64" or just "amd64"). A multi-arch image attaches one SBOM
	// per platform; this picks exactly one. Required when the referenced image is
	// multi-arch; ignored for single-platform images.
	Platform string `json:"platform,omitempty"`

	// Access carries the resolved access of the referenced resource. It is not
	// authored by hand: the CLI copies the subject resource's access here before
	// construction so the input method can run on-image SBOM discovery against it.
	Access *runtime.Raw `json:"access,omitempty"`
}

func (t *SBOM) String() string {
	if t.Resource != nil {
		return t.Resource.String()
	}
	return Type
}
