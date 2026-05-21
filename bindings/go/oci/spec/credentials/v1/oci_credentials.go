package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCICredentialsType is the type name for OCI registry credentials.
const OCICredentialsType = "OCICredentials"

var OCICredentialsVersionedType = runtime.NewVersionedType(OCICredentialsType, Version)

// OCICredentials represents typed credentials for OCI registry authentication.
// It supports username/password and token-based authentication flows used by
// container registries that implement the OCI distribution specification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCICredentials struct {
	// +ocm:jsonschema-gen:enum=OCICredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCICredentials
	Type         runtime.Type `json:"type"`
	Username     string       `json:"username,omitempty"`
	Password     string       `json:"password,omitempty"`
	AccessToken  string       `json:"accessToken,omitempty"`
	RefreshToken string       `json:"refreshToken,omitempty"`
}
