package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Identity is the typed parallel of the untyped runtime.Identity map.
// It carries the well-known attributes that uniquely identify an arbitrary
// resource (typically a target system reachable via URL components).
//
// Identity is intended as the structural successor of runtime.Identity and
// exposes the same well-known fields. Domain-specific identities (for example
// OCIRegistryIdentity or RSAIdentity) remain separate typed structs.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Identity struct {
	// +ocm:jsonschema-gen:enum=Identity/v1
	// +ocm:jsonschema-gen:enum:deprecated=Identity
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}
