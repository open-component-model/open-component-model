package v1

import (
	"encoding/json"
	"errors"
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

//nolint:gosec // G101: These are key names, not credentials.
const (
	CredentialKeyPublicKeyPEM      = "publicKeyPEM"
	CredentialKeyPublicKeyPEMFile  = "publicKeyPEMFile"
	CredentialKeyPrivateKeyPEM     = "privateKeyPEM"
	CredentialKeyPrivateKeyPEMFile = "privateKeyPEMFile"
)

// Deprecated snake_case aliases kept for backward compatibility with legacy .ocmconfig files.
// FromDirectCredentials accepts both camelCase and these deprecated keys.
//
//nolint:gosec // G101: These are key names, not credentials.
const (
	// Deprecated: Use CredentialKeyPublicKeyPEM instead.
	DeprecatedCredentialKeyPublicKeyPEM = "public_key_pem"
	// Deprecated: Use CredentialKeyPublicKeyPEMFile instead.
	DeprecatedCredentialKeyPublicKeyPEMFile = "public_key_pem_file"
	// Deprecated: Use CredentialKeyPrivateKeyPEM instead.
	DeprecatedCredentialKeyPrivateKeyPEM = "private_key_pem"
	// Deprecated: Use CredentialKeyPrivateKeyPEMFile instead.
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
	Type              runtime.Type `json:"type"`
	PublicKeyPEM      string       `json:"publicKeyPEM,omitempty"`
	PublicKeyPEMFile  string       `json:"publicKeyPEMFile,omitempty"`
	PrivateKeyPEM     string       `json:"privateKeyPEM,omitempty"`
	PrivateKeyPEMFile string       `json:"privateKeyPEMFile,omitempty"`
}

// MustRegisterCredentialType registers RSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&RSACredentials{},
		runtime.NewVersionedType(RSACredentialsType, Version),
		runtime.NewUnversionedType(RSACredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// This supports old .ocmconfig files that use Credentials/v1 with RSA properties.
// A nil map is safe and returns an RSACredentials with only the type set.
// FromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// This supports old .ocmconfig files that use Credentials/v1 with RSA properties.
// Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns an RSACredentials with only the type set.
// FromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// This supports old .ocmconfig files that use Credentials/v1 with RSA properties.
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

// FromTyped converts runtime.Typed into RSACredentials.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
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
		RSACredentials := RSACredentials{}
		if err := Scheme.Convert(creds, &RSACredentials); err != nil {
			return nil, fmt.Errorf("error converting raw credentials to RSACredentials: %w", err)
		}
		return &RSACredentials, nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		RSACredentials := RSACredentials{}
		if err := json.Unmarshal(data, &RSACredentials); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to RSACredentials: %w", err)
		}
		return &RSACredentials, nil
	}

	slog.Error("unexpected credential type, expected RSACredentials or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
