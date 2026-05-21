package v1alpha1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// camelCase JSON property keys — primary form expected on DirectCredentials.
	credentialKeyTrustedRootJSON     = "trustedRootJSON"
	credentialKeyTrustedRootJSONFile = "trustedRootJSONFile"

	// Legacy snake_case aliases accepted from .ocmconfig as fallback.
	// TODO(matthiasbruns): https://github.com/open-component-model/ocm-project/issues/1072
	deprecatedCredentialKeyTrustedRootJSON     = "trusted_root_json"
	deprecatedCredentialKeyTrustedRootJSONFile = "trusted_root_json_file"
)

// convertScheme is a private scheme that knows TrustedRoot and DirectCredentials.
// It deliberately does NOT register sigstore's OIDCIdentityToken type: an OIDCIdentityToken
// credential passed into the Verify path is rejected here with a clear "unsupported type"
// error.
var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&TrustedRoot{},
		VersionedType,
		runtime.NewUnversionedType(TrustedRootType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToTrustedRoot converts [runtime.Typed] into [TrustedRoot].
// Direct conversion as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations (for example a sigstore OIDCIdentityToken
// credential mistakenly resolved for a Verify call), an error is returned.
func ConvertToTrustedRoot(creds runtime.Typed) (*TrustedRoot, error) {
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
	case *TrustedRoot:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

// fromDirectCredentials converts a DirectCredentials properties map into a typed TrustedRoot.
// Both camelCase and the deprecated snake_case keys are accepted.
// A nil map is safe and returns a TrustedRoot with only the type set.
func fromDirectCredentials(properties map[string]string) *TrustedRoot {
	return &TrustedRoot{
		Type:                VersionedType,
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
