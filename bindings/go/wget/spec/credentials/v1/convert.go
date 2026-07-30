package v1

import (
	"fmt"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyUsername             = "username"
	credentialKeyPassword             = "password"
	credentialKeyIdentityToken        = "identityToken"
	credentialKeyCertificate          = "certificate"
	credentialKeyPrivateKey           = "privateKey"
	credentialKeyCertificateAuthority = "certificateAuthority"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&WgetCredentials{},
		WgetCredentialsVersionedType,
		runtime.NewUnversionedType(WgetCredentialsType),
	)
	directcredsv1.MustRegister(convertScheme)
}

// fromDirectCredentials converts a DirectCredentials properties map into typed WgetCredentials.
// This supports legacy .ocmconfig files that use Credentials/v1 with wget properties.
func fromDirectCredentials(properties map[string]string) *WgetCredentials {
	return &WgetCredentials{
		Type:                 runtime.NewVersionedType(WgetCredentialsType, Version),
		Username:             properties[credentialKeyUsername],
		Password:             properties[credentialKeyPassword],
		IdentityToken:        properties[credentialKeyIdentityToken],
		Certificate:          properties[credentialKeyCertificate],
		PrivateKey:           properties[credentialKeyPrivateKey],
		CertificateAuthority: properties[credentialKeyCertificateAuthority],
	}
}

// ConvertToWgetCredentials converts runtime.Typed into WgetCredentials.
// Direct conversion as well as converting from DirectCredentials/v1 is supported.
func ConvertToWgetCredentials(creds runtime.Typed) (*WgetCredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *directcredsv1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *WgetCredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}
