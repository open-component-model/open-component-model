package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	//nolint:gosec // G101: This is a type name, not a credential.
	HelmHTTPCredentialsType = "HelmHTTPCredentials"
	Version                 = "v1"
)

// HelmHTTPCredentials represents typed credentials for HTTP/S-based Helm chart repositories.
// For OCI-based Helm repositories, use OCICredentials/v1 instead.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmHTTPCredentials struct {
	// +ocm:jsonschema-gen:enum=HelmHTTPCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=HelmHTTPCredentials
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
	CertFile string       `json:"certFile,omitempty"`
	KeyFile  string       `json:"keyFile,omitempty"`
	Keyring  string       `json:"keyring,omitempty"`
}

// MustRegisterCredentialType registers HelmHTTPCredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&HelmHTTPCredentials{},
		runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		runtime.NewUnversionedType(HelmHTTPCredentialsType),
	)
}
