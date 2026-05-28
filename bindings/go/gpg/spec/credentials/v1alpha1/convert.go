package v1alpha1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	credentialKeyPrivateKeyPGP     = "privateKeyPGP"
	credentialKeyPrivateKeyPGPFile = "privateKeyPGPFile"
	credentialKeyPublicKeyPGP      = "publicKeyPGP"
	credentialKeyPublicKeyPGPFile  = "publicKeyPGPFile"
	credentialKeyPassphrase        = "passphrase"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&GPGCredentials{},
		runtime.NewVersionedType(GPGCredentialsType, Version),
		runtime.NewUnversionedType(GPGCredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// ConvertToGPGCredentials converts [runtime.Typed] into [GPGCredentials].
// Direct conversion as well as converting from [v1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToGPGCredentials(creds runtime.Typed) (*GPGCredentials, error) {
	if t, ok := creds.(*GPGCredentials); ok {
		return t, nil
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
	case *GPGCredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

func fromDirectCredentials(properties map[string]string) *GPGCredentials {
	return &GPGCredentials{
		Type:              runtime.NewVersionedType(GPGCredentialsType, Version),
		PrivateKeyPGP:     properties[credentialKeyPrivateKeyPGP],
		PrivateKeyPGPFile: properties[credentialKeyPrivateKeyPGPFile],
		PublicKeyPGP:      properties[credentialKeyPublicKeyPGP],
		PublicKeyPGPFile:  properties[credentialKeyPublicKeyPGPFile],
		Passphrase:        properties[credentialKeyPassphrase],
	}
}
