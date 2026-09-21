package v1

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/runtime"
)

var WgetCredentialsVersionedType = runtime.NewVersionedType(WgetCredentialsType, Version)

// MustRegisterCredentialType registers WgetCredentials/v1 (and its unversioned
// alias) in scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&WgetCredentials{},
		WgetCredentialsVersionedType,
		runtime.NewUnversionedType(WgetCredentialsType),
	)
}

// WgetCredentials carries typed credentials for wget authentication.
//
// mTLS (Certificate + PrivateKey) is transport-layer and composes with either
// header-based method. Username/Password (Basic) and IdentityToken (Bearer)
// both set the Authorization header and are mutually exclusive; IdentityToken
// wins when both are set.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type WgetCredentials struct {
	// +ocm:jsonschema-gen:enum=WgetCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=WgetCredentials
	Type runtime.Type `json:"type"`
	// Username for HTTP Basic Auth (paired with Password). Ignored if
	// IdentityToken is set.
	Username string `json:"username,omitempty"`
	// Password for HTTP Basic Auth (paired with Username).
	Password string `json:"password,omitempty"`
	// IdentityToken is sent as "Authorization: Bearer <token>". Takes
	// precedence over Username/Password.
	IdentityToken string `json:"identityToken,omitempty"`
	// Certificate is a PEM client certificate for mTLS (requires PrivateKey).
	Certificate string `json:"certificate,omitempty"`
	// PrivateKey is the PEM key paired with Certificate.
	PrivateKey string `json:"privateKey,omitempty"`
	// CertificateAuthority is an optional PEM CA used to verify the server
	// certificate. Only meaningful when Certificate is set.
	CertificateAuthority string `json:"certificateAuthority,omitempty"`
}

var _ runtime.Validatable = (*WgetCredentials)(nil)

// Validate rejects credentials with no usable authentication material or that
// violate field-pairing rules.
func (c *WgetCredentials) Validate() error {
	if c.Password != "" && c.Username == "" {
		return errors.New("password is set but username is empty: basic authentication requires both username and password")
	}
	if c.PrivateKey != "" && c.Certificate == "" {
		return errors.New("privateKey is set but certificate is empty: mTLS requires both certificate and privateKey")
	}
	if c.Certificate != "" && c.PrivateKey == "" {
		return errors.New("certificate is set but privateKey is empty: mTLS requires both certificate and privateKey")
	}
	if c.CertificateAuthority != "" && c.Certificate == "" {
		return errors.New("certificateAuthority is set but certificate is empty: the certificate authority is only evaluated together with a client certificate")
	}
	if c.IdentityToken == "" && c.Username == "" && c.Certificate == "" {
		return errors.New("no authentication material: set at least one of identityToken, username/password, or certificate/privateKey")
	}
	return nil
}
