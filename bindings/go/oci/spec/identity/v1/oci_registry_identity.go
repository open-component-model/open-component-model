package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OCIRegistryIdentityType = "OCIRegistry"
	Version                 = "v1"
)

// Type is the unversioned Consumer Identity type for any OCI Repository (backward compat).
var Type = runtime.NewUnversionedType(OCIRegistryIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(OCIRegistryIdentityType, Version)

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
	Type runtime.Type `json:"type"`
	// Hostname is the registry hostname (e.g. "ghcr.io", "registry.example.com").
	// Primary matching attribute when resolving credentials for a registry request.
	// Omit to match any hostname.
	Hostname string `json:"hostname,omitempty"`
	// Scheme is the URL scheme used to connect to the registry ("https" or "http").
	// When set to "http", the client connects without TLS (plain HTTP).
	// Omit to match both schemes.
	Scheme string `json:"scheme,omitempty"`
	// Port is the registry port as a string (e.g. "443", "5000").
	// Used together with Hostname to distinguish registries on non-standard ports.
	// Omit to match any port.
	Port string `json:"port,omitempty"`
	// Path is a path prefix within the registry (e.g. "/myorg/myrepo").
	// Narrows credential matching to a specific repository or namespace inside the registry.
	// Omit to match all paths on the registry.
	Path string `json:"path,omitempty"`
}
