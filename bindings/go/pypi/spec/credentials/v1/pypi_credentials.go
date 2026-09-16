package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// PyPICredentials represents typed credentials for a PyPI repository.
//
// PyPI indexes (PyPI, TestPyPI, Nexus, Artifactory, GitHub Packages)
// authenticate with HTTP Basic Auth or a bearer token, and nothing else in
// practice. API tokens are used as Basic Auth with the literal username
// "__token__" and the token as the password. Both forms set the Authorization
// header, so they are mutually exclusive; IdentityToken takes precedence when
// both are set.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type PyPICredentials struct {
	// +ocm:jsonschema-gen:enum=PyPICredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=PyPICredentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	// For an API token, set this to "__token__". Ignored if IdentityToken is set.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	// Takes precedence over Username/Password when set.
	IdentityToken string `json:"identityToken,omitempty"`
}

// MustRegisterCredentialType registers PyPICredentials/v1 (and its unversioned
// alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&PyPICredentials{},
		runtime.NewVersionedType(PyPICredentialsType, Version),
		runtime.NewUnversionedType(PyPICredentialsType),
	)
}
