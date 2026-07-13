package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var WgetCredentialsVersionedType = runtime.NewVersionedType(WgetCredentialsType, Version)

// MustRegisterCredentialType registers WgetCredentials/v1 (and its unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&WgetCredentials{},
		WgetCredentialsVersionedType,
		runtime.NewUnversionedType(WgetCredentialsType),
	)
}

// WgetCredentials represents typed credentials for wget access type authentication.
//
// The mTLS client certificate (Certificate + PrivateKey) is a transport-layer
// credential and is applied independently, so it can be combined with either of
// the header-based authentication methods.
//
// Username/Password (HTTP Basic Auth) and IdentityToken (Bearer token) both set
// the Authorization header and are therefore mutually exclusive; IdentityToken
// takes precedence when both are set.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type WgetCredentials struct {
	// +ocm:jsonschema-gen:enum=WgetCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=WgetCredentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	// Ignored if IdentityToken is set.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	// Takes precedence over Username/Password when set.
	IdentityToken string `json:"identityToken,omitempty"`
	// Certificate is a PEM-encoded client certificate for mTLS authentication.
	// Requires PrivateKey. Applied independently of Basic/Bearer authentication.
	Certificate string `json:"certificate,omitempty"`
	// PrivateKey is a PEM-encoded private key paired with Certificate for mTLS.
	PrivateKey string `json:"privateKey,omitempty"`
	// CertificateAuthority is an optional PEM-encoded CA certificate used to verify
	// the server's TLS certificate. Only used when Certificate is set.
	CertificateAuthority string `json:"certificateAuthority,omitempty"`
}
