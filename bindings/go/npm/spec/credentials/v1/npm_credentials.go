package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var NPMCredentialsVersionedType = runtime.NewVersionedType(NPMCredentialsType, Version)

// NPMCredentials represents typed credentials for an npm registry.
//
// Username/Password (HTTP basic auth) and Token (bearer token, the value an
// npm login writes to .npmrc as _authToken) both set the Authorization header
// and are therefore mutually exclusive; a complete Username/Password pair takes
// precedence over Token, matching OCM v1 downloads.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NPMCredentials struct {
	// +ocm:jsonschema-gen:enum=NPMCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=NPMCredentials
	Type runtime.Type `json:"type"`
	// Username is the user name for HTTP basic authentication. Used together
	// with Password. A complete pair takes precedence over Token.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP basic authentication. Used together
	// with Username.
	Password string `json:"password,omitempty"`
	// Email is the address some npm registries require for a login. It is not
	// needed to read a package.
	Email string `json:"email,omitempty"`
	// Token is a bearer token sent as "Authorization: Bearer <token>".
	// Used when a complete Username/Password pair is not available.
	Token string `json:"token,omitempty"`
}

// MustRegisterCredentialType registers NPMCredentials/v1 (and its unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&NPMCredentials{},
		NPMCredentialsVersionedType,
		runtime.NewUnversionedType(NPMCredentialsType),
	)
}
