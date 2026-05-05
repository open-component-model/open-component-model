package v1

import (
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
