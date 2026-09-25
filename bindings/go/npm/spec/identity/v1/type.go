package v1

import (
	"errors"
	"net/url"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// NPMRegistryIdentityType is the consumer identity type of the OCM v1 npm
	// registry, kept so credentials configured for OCM v1 still match.
	NPMRegistryIdentityType = "NpmRegistry"
	Version                 = "v1"
)

// Type is the unversioned consumer identity type for npm registries (backward compat).
var Type = runtime.NewUnversionedType(NPMRegistryIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(NPMRegistryIdentityType, Version)

// NPMRegistryIdentity is the typed consumer identity for npm packages. It is
// derived from the registry URL joined with the package name, so credentials
// can be configured per registry or narrowed down to a scope or a single
// package by path.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NPMRegistryIdentity struct {
	// +ocm:jsonschema-gen:enum=NpmRegistry/v1
	// +ocm:jsonschema-gen:enum:deprecated=NpmRegistry
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// IdentityFromRegistryAndPackage derives the credential consumer identity of an
// npm package from its registry URL and package name, as OCM v1 does. The
// unversioned [Type] is used, consistent with the OCIRegistry and Wget consumer
// identities.
func IdentityFromRegistryAndPackage(registry, pkg string) (runtime.Identity, error) {
	if registry == "" {
		return nil, errors.New("registry is required")
	}

	joined, err := url.JoinPath(registry, pkg)
	if err != nil {
		return nil, err
	}

	identity, err := runtime.ParseURLToIdentity(joined)
	if err != nil {
		return nil, err
	}
	identity.SetType(Type)

	return identity, nil
}
