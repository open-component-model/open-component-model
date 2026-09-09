package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// GitCredentials supports HTTP basic authentication, bearer tokens, and SSH keys.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitCredentials struct {
	// +ocm:jsonschema-gen:enum=GitCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=GitCredentials
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	// Password is the HTTP password or the SSH key passphrase.
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	// PrivateKey is a file path, as in OCM v1.
	PrivateKey string `json:"privateKey,omitempty"`
}

func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitCredentials{},
		runtime.NewVersionedType(GitCredentialsType, Version),
		runtime.NewUnversionedType(GitCredentialsType),
	)
}
