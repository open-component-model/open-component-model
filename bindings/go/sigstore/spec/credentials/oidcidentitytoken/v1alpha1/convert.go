package v1alpha1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// camelCase JSON property keys — primary form expected on DirectCredentials.
	credentialKeyToken     = "token"
	credentialKeyTokenFile = "tokenFile"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&OIDCIdentityToken{},
		VersionedType,
		runtime.NewUnversionedType(OIDCIdentityTokenType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToOIDCIdentityToken converts [runtime.Typed] into [OIDCIdentityToken].
// Direct conversion as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToOIDCIdentityToken(creds runtime.Typed) (*OIDCIdentityToken, error) {
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
	case *OIDCIdentityToken:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

// fromDirectCredentials converts a DirectCredentials properties map into a typed OIDCIdentityToken.
// A nil map is safe and returns an OIDCIdentityToken with only the type set.
func fromDirectCredentials(properties map[string]string) *OIDCIdentityToken {
	return &OIDCIdentityToken{
		Type:      VersionedType,
		Token:     properties[credentialKeyToken],
		TokenFile: properties[credentialKeyTokenFile],
	}
}
