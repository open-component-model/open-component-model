package v1

import (
	"fmt"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	credentialKeyUsername = "username"
	credentialKeyPassword = "password"
	credentialKeyCertFile = "certFile"
	credentialKeyKeyFile  = "keyFile"
	credentialKeyKeyring  = "keyring"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&HelmHTTPCredentials{},
		runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		runtime.NewUnversionedType(HelmHTTPCredentialsType),
	)
	credv1.MustRegister(convertScheme)
}

// ConvertToHelmHTTPCredentials converts [runtime.Typed] into [HelmHTTPCredentials].
// Direct conversion as well as converting from [credv1.DirectCredentials] is supported.
// Other supported [runtime.Typed] implementations are [runtime.Raw].
// For unsupported [runtime.Typed] implementations, an error will be returned.
func ConvertToHelmHTTPCredentials(creds runtime.Typed) (*HelmHTTPCredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *credv1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *HelmHTTPCredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}

func fromDirectCredentials(properties map[string]string) *HelmHTTPCredentials {
	return &HelmHTTPCredentials{
		Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		Username: properties[credentialKeyUsername],
		Password: properties[credentialKeyPassword],
		CertFile: properties[credentialKeyCertFile],
		KeyFile:  properties[credentialKeyKeyFile],
		Keyring:  properties[credentialKeyKeyring],
	}
}
