package v1alpha1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	// camelCase JSON property keys — primary form expected on DirectCredentials.
	credentialKeyRootCertsPEM     = "rootCertsPEM"
	credentialKeyRootCertsPEMFile = "rootCertsPEMFile"

	// Legacy snake_case aliases accepted as a deprecated fallback. These match the
	// keys used by OCM v1 .ocmconfig files and existing TSA/v1alpha1 consumer
	// entries, so they continue to resolve unchanged.
	// TODO(matthiasbruns): https://github.com/open-component-model/ocm-project/issues/1072
	deprecatedCredentialKeyRootCertsPEM     = "root_certs_pem"
	deprecatedCredentialKeyRootCertsPEMFile = "root_certs_pem_file"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&TSACredentials{},
		VersionedType,
		runtime.NewUnversionedType(TSACredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToTSACredentials converts [runtime.Typed] into [TSACredentials].
// Direct conversion as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToTSACredentials(creds runtime.Typed) (*TSACredentials, error) {
	if creds == nil {
		return &TSACredentials{Type: VersionedType}, nil
	}

	typ := creds.GetType()
	if typ.IsEmpty() {
		var err error
		typ, err = convertScheme.TypeForPrototype(creds)
		if err != nil {
			return nil, fmt.Errorf("error converting credential type: %w", err)
		}
	}
	typed, err := convertScheme.NewObject(typ)
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *v1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *TSACredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

// fromDirectCredentials converts a DirectCredentials properties map into typed
// TSACredentials. Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns a TSACredentials with only the type set.
func fromDirectCredentials(properties map[string]string) *TSACredentials {
	return &TSACredentials{
		Type:             VersionedType,
		RootCertsPEM:     lookupProperty(properties, credentialKeyRootCertsPEM, deprecatedCredentialKeyRootCertsPEM),
		RootCertsPEMFile: lookupProperty(properties, credentialKeyRootCertsPEMFile, deprecatedCredentialKeyRootCertsPEMFile),
	}
}

// lookupProperty returns the value for key, falling back to the deprecated key
// when the primary is unset.
func lookupProperty(properties map[string]string, key, deprecated string) string {
	if v := properties[key]; v != "" {
		return v
	}
	return properties[deprecated]
}
