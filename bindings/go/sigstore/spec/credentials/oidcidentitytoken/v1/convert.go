package v1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	CredentialKeyToken     = "token"
	CredentialKeyTokenFile = "tokenFile"
)

// Deprecated: Use CredentialKeyTokenFile instead.
const DeprecatedCredentialKeyTokenFile = "token_file"

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&OIDCIdentityToken{},
		OIDCIdentityTokenVersionedType,
		runtime.NewUnversionedType(OIDCIdentityTokenType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToOIDCIdentityToken converts runtime.Typed into OIDCIdentityToken.
// Direct conversation as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToOIDCIdentityToken(creds runtime.Typed) (*OIDCIdentityToken, error) {
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
	case *OIDCIdentityToken:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

func fromDirectCredentials(properties map[string]string) *OIDCIdentityToken {
	return &OIDCIdentityToken{
		Type:      runtime.NewVersionedType(OIDCIdentityTokenType, Version),
		Token:     properties[CredentialKeyToken],
		TokenFile: lookupProperty(properties, CredentialKeyTokenFile, DeprecatedCredentialKeyTokenFile),
	}
}

func lookupProperty(properties map[string]string, key, deprecated string) string {
	if v := properties[key]; v != "" {
		return v
	}
	return properties[deprecated]
}
