package v1

import (
	"log/slog"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	WgetIdentityType = "Wget"
	Version          = "v1"
)

// Type is the unversioned consumer identity type for wget resources (backward compat).
var Type = runtime.NewUnversionedType(WgetIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(WgetIdentityType, Version)

// WgetIdentity is the typed consumer identity for resources fetched over HTTP/S,
// covering both the wget access type and the wget constructor input method. It is
// derived from the resource URL.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type WgetIdentity struct {
	// +ocm:jsonschema-gen:enum=Wget/v1
	// +ocm:jsonschema-gen:enum:deprecated=Wget
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// ToIdentity converts a [WgetIdentity] into a [runtime.Identity].
// Empty fields are omitted from the resulting map. If the type field is unset,
// the canonical [VersionedType] is used.
func ToIdentity(identity *WgetIdentity) runtime.Identity {
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

// FromIdentity converts a [runtime.Identity] into a [WgetIdentity].
// Attributes outside the wget identity schema are ignored. If the type
// attribute is missing or empty, the canonical [VersionedType] is used.
func FromIdentity(id runtime.Identity) *WgetIdentity {
	if id == nil {
		return nil
	}
	out := &WgetIdentity{
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

// IdentityFromURL derives the credential consumer identity for a wget resource
// from its URL. The unversioned [Type] is used, consistent with the OCIRegistry
// and HelmChartRepository consumer identities.
func IdentityFromURL(url string) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(url)
	if err != nil {
		return nil, err
	}
	identity.SetType(Type)
	return identity, nil
}
