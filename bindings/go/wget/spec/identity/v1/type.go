package v1

import (
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
