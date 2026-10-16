package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	TSAIdentityType = "TSA"
	Version         = "v1alpha1"
)

// Type is the unversioned consumer identity type for RFC 3161 timestamping authorities.
var Type = runtime.NewUnversionedType(TSAIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(TSAIdentityType, Version)

// TSAIdentity is the typed consumer identity under which the root certificates of an
// RFC 3161 timestamping authority are resolved. It is derived from the TSA URL; an
// identity without URL attributes matches any TSA.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TSAIdentity struct {
	// +ocm:jsonschema-gen:enum=TSA/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=TSA
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}
