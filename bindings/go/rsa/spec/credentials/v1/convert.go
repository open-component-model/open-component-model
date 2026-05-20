package v1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
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

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&RSACredentials{},
		VersionedType,
		runtime.NewUnversionedType(RSACredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToRSACredentials converts [runtime.Typed] into [RSACredentials].
// Direct conversation as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToRSACredentials(creds runtime.Typed) (*RSACredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *v1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *RSACredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
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
