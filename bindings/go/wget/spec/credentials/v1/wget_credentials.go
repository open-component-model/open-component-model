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
// When multiple credential fields are set, they are applied in priority order:
//  1. Username + Password (HTTP Basic Auth) — highest priority
//  2. IdentityToken (Bearer token)
//  3. Certificate + PrivateKey (mTLS) — lowest priority
//
// Only the highest-priority non-empty credential is applied; lower-priority fields are ignored.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type WgetCredentials struct {
	// +ocm:jsonschema-gen:enum=WgetCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=WgetCredentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	// Takes priority over IdentityToken and Certificate when set.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	// Takes priority over Certificate when set. Ignored if Username is set.
	IdentityToken string `json:"identityToken,omitempty"`
	// Certificate is a PEM-encoded client certificate for mTLS authentication.
	// Requires PrivateKey. Ignored if Username or IdentityToken is set.
	Certificate string `json:"certificate,omitempty"`
	// PrivateKey is a PEM-encoded private key paired with Certificate for mTLS.
	PrivateKey string `json:"privateKey,omitempty"`
	// CertificateAuthority is an optional PEM-encoded CA certificate used to verify
	// the server's TLS certificate during mTLS. Only used when Certificate is set.
	CertificateAuthority string `json:"certificateAuthority,omitempty"`
}
