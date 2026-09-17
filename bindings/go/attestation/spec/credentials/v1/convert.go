package v1

import (
	"encoding/json"
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	credentialKeyPublicKeyPEM      = "publicKeyPEM"
	credentialKeyPublicKeyPEMFile  = "publicKeyPEMFile"
	credentialKeyPrivateKeyPEM     = "privateKeyPEM"
	credentialKeyPrivateKeyPEMFile = "privateKeyPEMFile"

	deprecatedCredentialKeyPublicKeyPEM      = "public_key_pem"
	deprecatedCredentialKeyPublicKeyPEMFile  = "public_key_pem_file"
	deprecatedCredentialKeyPrivateKeyPEM     = "private_key_pem"
	deprecatedCredentialKeyPrivateKeyPEMFile = "private_key_pem_file"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&ECDSACredentials{},
		VersionedType,
		runtime.NewUnversionedType(ECDSACredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToECDSACredentials converts a [runtime.Typed] into [ECDSACredentials].
// It supports direct ECDSACredentials, [v1.DirectCredentials], and untyped
// (empty-type) credentials produced by the credential graph's direct resolution
// when the ECDSA credential type is not registered in the credential type
// scheme.
func ConvertToECDSACredentials(creds runtime.Typed) (*ECDSACredentials, error) {
	if creds.GetType().IsEmpty() {
		if dc, ok := creds.(*v1.DirectCredentials); ok {
			return fromDirectCredentials(dc.Properties), nil
		}
		props, err := propertiesOf(creds)
		if err != nil {
			return nil, err
		}
		return fromDirectCredentials(props), nil
	}

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
	case *ECDSACredentials:
		return t, nil
	}
	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

// propertiesOf extracts a flat string map from an untyped credential value by
// round-tripping it through JSON. The "type" key, if present, is dropped.
func propertiesOf(creds runtime.Typed) (map[string]string, error) {
	raw, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("marshalling untyped credentials failed: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("unmarshalling untyped credentials failed: %w", err)
	}
	props := make(map[string]string, len(fields))
	for k, v := range fields {
		if k == "type" || v == nil {
			continue
		}
		props[k] = fmt.Sprint(v)
	}
	return props, nil
}

// fromDirectCredentials converts a DirectCredentials properties map into typed
// ECDSACredentials. Both camelCase and deprecated snake_case keys are accepted.
func fromDirectCredentials(properties map[string]string) *ECDSACredentials {
	return &ECDSACredentials{
		Type:              VersionedType,
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
