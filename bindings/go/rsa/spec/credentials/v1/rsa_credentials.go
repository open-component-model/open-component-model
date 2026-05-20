package v1

import (
	"encoding/json"
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// RSACredentialsType is the type name for RSA credentials.
	RSACredentialsType = "RSACredentials"
	// Version is the version of the RSA credentials type.
	Version = "v1"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	// camelCase JSON property keys — used by FromDirectCredentials.
	credentialKeyPublicKeyPEM      = "publicKeyPEM"
	credentialKeyPublicKeyPEMFile  = "publicKeyPEMFile"
	credentialKeyPrivateKeyPEM     = "privateKeyPEM"
	credentialKeyPrivateKeyPEMFile = "privateKeyPEMFile"

	// Legacy snake_case aliases from .ocmconfig files, accepted as fallback.
	// TODO(matthiasbruns): https://github.com/open-component-model/ocm-project/issues/1072
	deprecatedCredentialKeyPublicKeyPEM      = "public_key_pem"
	deprecatedCredentialKeyPublicKeyPEMFile  = "public_key_pem_file"
	deprecatedCredentialKeyPrivateKeyPEM     = "private_key_pem"
	deprecatedCredentialKeyPrivateKeyPEMFile = "private_key_pem_file"
)

var VersionedType = runtime.NewVersionedType(RSACredentialsType, Version)

// RSACredentials holds key material for RSA signing and/or verification.
//
// Each field has two forms: inline PEM content (PEM field) or a file path (PEMFile field).
// The inline form takes precedence when both are set.
//
// Signing requires PrivateKeyPEM or PrivateKeyPEMFile.
// For PEM-encoded signing, PublicKeyPEM or PublicKeyPEMFile should contain the certificate
// chain (leaf + intermediates) to embed in the signature.
//
// Verification of plain signatures requires PublicKeyPEM or PublicKeyPEMFile.
// If absent, the public key is derived from the private key.
// Verification of PEM-encoded signatures uses PublicKeyPEM or PublicKeyPEMFile as an
// optional trust anchor; if absent, the system root pool is used.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RSACredentials struct {
	// +ocm:jsonschema-gen:enum=RSACredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=RSACredentials
	Type runtime.Type `json:"type"`
	// PublicKeyPEM is an inline PEM-encoded RSA public key or X.509 certificate chain.
	// For plain signature verification: the signer's public key; derived from PrivateKeyPEM if absent.
	// For PEM-encoded signing: the certificate chain (leaf + intermediates) to embed in the signature.
	// For PEM-encoded signature verification: optional trust anchor; if absent, system roots are used.
	// Takes precedence over PublicKeyPEMFile when both are set.
	PublicKeyPEM string `json:"publicKeyPEM,omitempty"`
	// PublicKeyPEMFile is a path to a PEM file containing an RSA public key or X.509 certificate chain.
	// Same semantics as PublicKeyPEM, but loaded from disk. Ignored when PublicKeyPEM is also set.
	PublicKeyPEMFile string `json:"publicKeyPEMFile,omitempty"`
	// PrivateKeyPEM is an inline PEM-encoded RSA private key (PKCS#1 or PKCS#8).
	// Required for signing; not used during verification.
	// Takes precedence over PrivateKeyPEMFile when both are set.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// PrivateKeyPEMFile is a path to a PEM file containing an RSA private key (PKCS#1 or PKCS#8).
	// Same semantics as PrivateKeyPEM, but loaded from disk. Ignored when PrivateKeyPEM is also set.
	PrivateKeyPEMFile string `json:"privateKeyPEMFile,omitempty"`
}

// ConvertToRSACredentials converts [runtime.Typed] into [RSACredentials].
// Direct conversation as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToRSACredentials(creds runtime.Typed) (*RSACredentials, error) {
	switch t := creds.(type) {
	case *v1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		props := map[string]string{}
		if err := json.Unmarshal(t.Data, &props); err != nil {
			return nil, fmt.Errorf("error unmarshalling raw RSA credentials: %w", err)
		}
		return fromDirectCredentials(props), nil
	}

	rsaCreds := RSACredentials{}
	if err := Scheme.Convert(creds, &rsaCreds); err != nil {
		return nil, err
	}
	return &rsaCreds, nil
}

// fromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns an RSACredentials with only the type set.
func fromDirectCredentials(properties map[string]string) *RSACredentials {
	return &RSACredentials{
		Type:              runtime.NewVersionedType(RSACredentialsType, Version),
		PublicKeyPEM:      lookupProperty(properties, credentialKeyPublicKeyPEM, deprecatedCredentialKeyPublicKeyPEM),
		PublicKeyPEMFile:  lookupProperty(properties, credentialKeyPublicKeyPEMFile, deprecatedCredentialKeyPublicKeyPEMFile),
		PrivateKeyPEM:     lookupProperty(properties, credentialKeyPrivateKeyPEM, deprecatedCredentialKeyPrivateKeyPEM),
		PrivateKeyPEMFile: lookupProperty(properties, credentialKeyPrivateKeyPEMFile, deprecatedCredentialKeyPrivateKeyPEMFile),
	}
}

func lookupProperty(properties map[string]string, key, deprecated string) string {
	if v := properties[key]; v != "" {
		return v
	}
	return properties[deprecated]
}
