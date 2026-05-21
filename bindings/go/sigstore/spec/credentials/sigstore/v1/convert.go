package v1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyToken               = "token"
	credentialKeyTokenFile           = "tokenFile"
	credentialKeyTrustedRootJSON     = "trustedRootJSON"
	credentialKeyTrustedRootJSONFile = "trustedRootJSONFile"
)

const (
	deprecatedCredentialKeyTokenFile           = "token_file"
	deprecatedCredentialKeyTrustedRootJSON     = "trusted_root_json"
	deprecatedCredentialKeyTrustedRootJSONFile = "trusted_root_json_file"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&SigstoreCredentials{},
		SigstoreCredentialsVersionedType,
		runtime.NewUnversionedType(SigstoreCredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToSigstoreCredentials converts runtime.Typed into SigstoreCredentials.
// Direct conversation as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToSigstoreCredentials(creds runtime.Typed) (*SigstoreCredentials, error) {
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
	case *SigstoreCredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

func fromDirectCredentials(properties map[string]string) *SigstoreCredentials {
	return &SigstoreCredentials{
		Type:                runtime.NewVersionedType(SigstoreCredentialsType, Version),
		Token:               properties[credentialKeyToken],
		TokenFile:           lookupProperty(properties, credentialKeyTokenFile, deprecatedCredentialKeyTokenFile),
		TrustedRootJSON:     lookupProperty(properties, credentialKeyTrustedRootJSON, deprecatedCredentialKeyTrustedRootJSON),
		TrustedRootJSONFile: lookupProperty(properties, credentialKeyTrustedRootJSONFile, deprecatedCredentialKeyTrustedRootJSONFile),
	}
}

func lookupProperty(properties map[string]string, key, deprecated string) string {
	if v := properties[key]; v != "" {
		return v
	}
	return properties[deprecated]
}
