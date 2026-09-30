package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// GitCredentials supports HTTP basic authentication, bearer tokens, and SSH keys.
// An SSH repository without a private key uses the SSH agent.
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
	// PrivateKey is a path to an SSH private key file, as in OCM v1.
	// Ignored when PrivateKeyPEM is also set.
	PrivateKey string `json:"privateKey,omitempty"`
	// PrivateKeyPEM is an inline PEM-encoded SSH private key.
	// Takes precedence over PrivateKey when both are set.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
}

func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitCredentials{},
		runtime.NewVersionedType(GitCredentialsType, Version),
		runtime.NewUnversionedType(GitCredentialsType),
	)
}
