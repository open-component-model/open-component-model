package v1

import (
	"encoding/json"
	"fmt"
	"log/slog"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// RSACredentialsType is the type name for RSA credentials.
	RSACredentialsType = "RSACredentials"
	// Version is the version of the RSA credentials type.
	Version = "v1"
)

const (
	// CredentialKeyPublicKeyPEM is the key for an inline PEM-encoded RSA public key or X.509 certificate chain.
	// For plain signatures it supplies the verifier's public key.
	// For PEM-encoded signatures it provides the trust anchor (root CA or leaf certificate) used during chain validation.
	// Takes precedence over CredentialKeyPublicKeyPEMFile when both are set.
	// If absent during signing, the public key is derived automatically from the private key.
	CredentialKeyPublicKeyPEM = "publicKeyPEM"
	// CredentialKeyPublicKeyPEMFile is the key for a path to a PEM file containing an RSA public key or X.509 certificate chain.
	// Semantics are identical to CredentialKeyPublicKeyPEM; used when the PEM material is stored on disk rather than inline.
	// Ignored when CredentialKeyPublicKeyPEM is also set.
	CredentialKeyPublicKeyPEMFile = "publicKeyPEMFile"
	// CredentialKeyPrivateKeyPEM is the key for an inline PEM-encoded RSA private key (PKCS#1 or PKCS#8 format).
	// Required for signing; not used during verification.
	// Takes precedence over CredentialKeyPrivateKeyPEMFile when both are set.
	CredentialKeyPrivateKeyPEM = "privateKeyPEM"
	// CredentialKeyPrivateKeyPEMFile is the key for a path to a PEM file containing an RSA private key (PKCS#1 or PKCS#8 format).
	// Semantics are identical to CredentialKeyPrivateKeyPEM; used when the key is stored on disk rather than inline.
	// Ignored when CredentialKeyPrivateKeyPEM is also set.
	CredentialKeyPrivateKeyPEMFile = "privateKeyPEMFile"
)

// Deprecated snake_case aliases kept for backward compatibility with legacy .ocmconfig files.
// FromDirectCredentials accepts both camelCase and these deprecated keys.
//
//nolint:gosec // G101: These are key names, not credentials.
const (
	// Deprecated: Use CredentialKeyPublicKeyPEM instead.
	// Legacy snake_case key for an inline PEM-encoded RSA public key or X.509 certificate chain.
	// Semantics are identical to CredentialKeyPublicKeyPEM; accepted by FromDirectCredentials for backward compatibility with legacy .ocmconfig files.
	DeprecatedCredentialKeyPublicKeyPEM = "public_key_pem"
	// Deprecated: Use CredentialKeyPublicKeyPEMFile instead.
	// Legacy snake_case key for a path to a PEM file containing an RSA public key or X.509 certificate chain.
	// Semantics are identical to CredentialKeyPublicKeyPEMFile; accepted by FromDirectCredentials for backward compatibility with legacy .ocmconfig files.
	DeprecatedCredentialKeyPublicKeyPEMFile = "public_key_pem_file"
	// Deprecated: Use CredentialKeyPrivateKeyPEM instead.
	// Legacy snake_case key for an inline PEM-encoded RSA private key (PKCS#1 or PKCS#8).
	// Semantics are identical to CredentialKeyPrivateKeyPEM; accepted by FromDirectCredentials for backward compatibility with legacy .ocmconfig files.
	DeprecatedCredentialKeyPrivateKeyPEM = "private_key_pem"
	// Deprecated: Use CredentialKeyPrivateKeyPEMFile instead.
	// Legacy snake_case key for a path to a PEM file containing an RSA private key (PKCS#1 or PKCS#8).
	// Semantics are identical to CredentialKeyPrivateKeyPEMFile; accepted by FromDirectCredentials for backward compatibility with legacy .ocmconfig files.
	DeprecatedCredentialKeyPrivateKeyPEMFile = "private_key_pem_file"
)

// RSACredentials represents typed credentials for RSA signing and verification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RSACredentials struct {
	// +ocm:jsonschema-gen:enum=RSACredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=RSACredentials
	Type runtime.Type `json:"type"`
	// PublicKeyPEM holds an inline PEM-encoded RSA public key or X.509 certificate chain.
	// See CredentialKeyPublicKeyPEM for full semantics and precedence rules.
	PublicKeyPEM string `json:"publicKeyPEM,omitempty"`
	// PublicKeyPEMFile is the path to a PEM file containing an RSA public key or X.509 certificate chain.
	// Used when CredentialKeyPublicKeyPEM is absent. See CredentialKeyPublicKeyPEMFile for full semantics.
	PublicKeyPEMFile string `json:"publicKeyPEMFile,omitempty"`
	// PrivateKeyPEM holds an inline PEM-encoded RSA private key (PKCS#1 or PKCS#8).
	// Required for signing. See CredentialKeyPrivateKeyPEM for precedence rules.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// PrivateKeyPEMFile is the path to a PEM file containing an RSA private key (PKCS#1 or PKCS#8).
	// Used when CredentialKeyPrivateKeyPEM is absent. See CredentialKeyPrivateKeyPEMFile for full semantics.
	PrivateKeyPEMFile string `json:"privateKeyPEMFile,omitempty"`
}

// MustRegisterCredentialType registers RSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&RSACredentials{},
		runtime.NewVersionedType(RSACredentialsType, Version),
		runtime.NewUnversionedType(RSACredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns an RSACredentials with only the type set.
func FromDirectCredentials(properties map[string]string) *RSACredentials {
	return &RSACredentials{
		Type:              runtime.NewVersionedType(RSACredentialsType, Version),
		PublicKeyPEM:      lookupProperty(properties, CredentialKeyPublicKeyPEM, DeprecatedCredentialKeyPublicKeyPEM),
		PublicKeyPEMFile:  lookupProperty(properties, CredentialKeyPublicKeyPEMFile, DeprecatedCredentialKeyPublicKeyPEMFile),
		PrivateKeyPEM:     lookupProperty(properties, CredentialKeyPrivateKeyPEM, DeprecatedCredentialKeyPrivateKeyPEM),
		PrivateKeyPEMFile: lookupProperty(properties, CredentialKeyPrivateKeyPEMFile, DeprecatedCredentialKeyPrivateKeyPEMFile),
	}
}

func lookupProperty(properties map[string]string, key, deprecated string) string {
	if v := properties[key]; v != "" {
		return v
	}
	return properties[deprecated]
}

// FromTyped converts [runtime.Typed] into RSACredentials.
// Direct conversation as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw] and [runtime.Unstructured].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func FromTyped(creds runtime.Typed) (*RSACredentials, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *RSACredentials:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		props := map[string]string{}
		if err := json.Unmarshal(t.Data, &props); err != nil {
			return nil, fmt.Errorf("error unmarshalling raw RSA credentials: %w", err)
		}
		return FromDirectCredentials(props), nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		props := map[string]string{}
		if err := json.Unmarshal(data, &props); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to RSACredentials: %w", err)
		}
		return FromDirectCredentials(props), nil
	}

	slog.Error("unexpected credential type, expected RSACredentials or DirectCredentials", "type", creds.GetType())
	return nil, fmt.Errorf("unexpected credential type: %T", creds)
}
