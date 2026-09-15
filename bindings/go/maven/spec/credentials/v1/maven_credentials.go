package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MavenCredentials represents typed credentials for a Maven repository.
//
// Maven repositories (Maven Central, Nexus, Artifactory, GitHub Packages)
// authenticate with HTTP Basic Auth or a bearer token, and nothing else in
// practice. Both set the Authorization header, so they are mutually
// exclusive; IdentityToken takes precedence when both are set.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MavenCredentials struct {
	// +ocm:jsonschema-gen:enum=MavenCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=MavenCredentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	// Ignored if IdentityToken is set.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	// Takes precedence over Username/Password when set.
	IdentityToken string `json:"identityToken,omitempty"`
}

// MustRegisterCredentialType registers MavenCredentials/v1 (and its
// unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&MavenCredentials{},
		runtime.NewVersionedType(MavenCredentialsType, Version),
		runtime.NewUnversionedType(MavenCredentialsType),
	)
}
