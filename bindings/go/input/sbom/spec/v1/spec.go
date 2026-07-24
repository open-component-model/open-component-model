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
// attached to another resource's OCI image at construction time and embeds it as
// a local blob in the component version, in its original format.
//
// The subject resource is referenced via Resource (name, optional version, and
// optional extraIdentity). The CLI resolves that reference to the subject's access
// before construction and fills Access, so the input method has a self-contained
// access to run discovery against. See the SBoM/v1 input method for details.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type SBOM struct {
	// +ocm:jsonschema-gen:enum=sbom/v1,SBoM/v1
	// +ocm:jsonschema-gen:enum:deprecated=sbom,SBoM
	Type runtime.Type `json:"type"`

	// Resource selects the resource whose SBOM should be discovered and embedded.
	Resource ResourceReference `json:"resource"`

	// Access carries the resolved access of the referenced resource. It is not
	// authored by hand: the CLI copies the subject resource's access here before
	// construction so the input method can run on-image SBOM discovery against it.
	Access *runtime.Raw `json:"access,omitempty"`
}

// ResourceReference selects a resource of the same component version by name,
// optional version, and optional extra identity attributes.
//
// For a multi-architecture OCI image, set extraIdentity.architecture (and,
// optionally, os/variant) to pick which platform's SBOM to embed. The name and
// version identify the subject resource; extraIdentity.architecture additionally
// selects the platform among the SBOMs attached to that image.
//
// +k8s:deepcopy-gen=true
type ResourceReference struct {
	// Name is the resource name (required).
	Name string `json:"name"`
	// Version is the optional resource version.
	Version string `json:"version,omitempty"`
	// ExtraIdentity holds additional identity attributes. For multi-arch images,
	// "architecture" (and optionally "os"/"variant") selects the platform's SBOM.
	ExtraIdentity runtime.Identity `json:"extraIdentity,omitempty"`
}

// Identity flattens the reference into an identity map (name + version), used to
// match the subject resource and to build the back-link label. The architecture
// in ExtraIdentity is a platform selector for the SBOM, not part of the OCI image
// resource's own identity, so it is intentionally excluded here.
func (r ResourceReference) Identity() runtime.Identity {
	id := runtime.Identity{}
	if r.Name != "" {
		id["name"] = r.Name
	}
	if r.Version != "" {
		id["version"] = r.Version
	}
	return id
}

// Architecture returns the platform selector from ExtraIdentity, or "" if unset.
func (r ResourceReference) Architecture() string {
	if r.ExtraIdentity == nil {
		return ""
	}
	return r.ExtraIdentity["architecture"]
}

func (t *SBOM) String() string {
	return t.Resource.Name
}
