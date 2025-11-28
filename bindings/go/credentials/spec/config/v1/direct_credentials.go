package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DirectCredentials represents a set of credentials that can be directly accessed
// through a map of key-value pairs. This struct is used to store and manage
// credential information in a simple, direct format.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DirectCredentials struct {
	// +ocm:jsonschema-gen:enum=DirectCredentials/v1,Credentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=Credentials,DirectCredentials,
	Type       runtime.Type      `json:"type"`
	Properties map[string]string `json:"properties"`
}
