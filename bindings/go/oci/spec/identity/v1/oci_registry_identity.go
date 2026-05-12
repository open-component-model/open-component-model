package v1

import (
	"log/slog"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIRegistryIdentity is the typed consumer identity for OCI container registries.
// It describes the target registry by hostname, scheme, port, and path.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIRegistryIdentity struct {
	// +ocm:jsonschema-gen:enum=OCIRegistry/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCIRegistry
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// ToIdentity converts an [OCIRegistryIdentity] into a [runtime.Identity].
// Empty fields are omitted from the resulting map. If the type field is unset,
// the canonical [VersionedType] is used.
func ToIdentity(identity *OCIRegistryIdentity) runtime.Identity {
	if identity == nil {
		return nil
	}
	id := runtime.Identity{}
	typ := identity.Type
	if typ.IsEmpty() {
		typ = VersionedType
	}
	id.SetType(typ)
	if identity.Hostname != "" {
		id[runtime.IdentityAttributeHostname] = identity.Hostname
	}
	if identity.Scheme != "" {
		id[runtime.IdentityAttributeScheme] = identity.Scheme
	}
	if identity.Port != "" {
		id[runtime.IdentityAttributePort] = identity.Port
	}
	if identity.Path != "" {
		id[runtime.IdentityAttributePath] = identity.Path
	}
	return id
}

// FromIdentity converts a [runtime.Identity] into an [OCIRegistryIdentity].
// Attributes outside the OCI registry schema are ignored. If the type
// attribute is missing or empty, the canonical [VersionedType] is used.
func FromIdentity(id runtime.Identity) *OCIRegistryIdentity {
	if id == nil {
		return nil
	}
	out := &OCIRegistryIdentity{
		Hostname: id[runtime.IdentityAttributeHostname],
		Scheme:   id[runtime.IdentityAttributeScheme],
		Port:     id[runtime.IdentityAttributePort],
		Path:     id[runtime.IdentityAttributePath],
	}

	typ, err := id.ParseType()
	if err != nil {
		slog.Debug("failed to parse identity type, defaulting to versioned type", "error", err)
	}
	if typ.IsEmpty() {
		typ = VersionedType
	}
	out.Type = typ
	return out
}
