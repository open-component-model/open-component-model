package v1

import (
	"fmt"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyUsername     = "username"
	credentialKeyPassword     = "password"
	credentialKeyCertFile     = "certFile"
	credentialKeyKeyFile      = "keyFile"
	credentialKeyKeyring      = "keyring"
	credentialKeyAccessToken  = "accessToken"
	credentialKeyRefreshToken = "refreshToken"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&HelmHTTPCredentials{},
		runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		runtime.NewUnversionedType(HelmHTTPCredentialsType),
	)
	convertScheme.MustRegisterWithAlias(&ocicredsv1.OCICredentials{},
		ocicredsv1.OCICredentialsVersionedType,
		runtime.NewUnversionedType(ocicredsv1.OCICredentialsType),
	)
	credv1.MustRegister(convertScheme)
}

func directToHelmHTTPCredentials(properties map[string]string) *HelmHTTPCredentials {
	return &HelmHTTPCredentials{
		Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		Username: properties[credentialKeyUsername],
		Password: properties[credentialKeyPassword],
		CertFile: properties[credentialKeyCertFile],
		KeyFile:  properties[credentialKeyKeyFile],
		Keyring:  properties[credentialKeyKeyring],
	}
}

func directToOCICredentials(properties map[string]string) *ocicredsv1.OCICredentials {
	return &ocicredsv1.OCICredentials{
		Type:         ocicredsv1.OCICredentialsVersionedType,
		Username:     properties[credentialKeyUsername],
		Password:     properties[credentialKeyPassword],
		AccessToken:  properties[credentialKeyAccessToken],
		RefreshToken: properties[credentialKeyRefreshToken],
	}
}

// ConvertToHelmHTTPCredentials converts runtime.Typed credentials into *HelmHTTPCredentials.
// DirectCredentials are mapped using the helm-relevant fields (username, password, certFile, keyFile, keyring).
// Returns nil, nil for nil input or input with an empty type.
func ConvertToHelmHTTPCredentials(creds runtime.Typed) (*HelmHTTPCredentials, error) {
	if creds == nil || creds.GetType().String() == "" {
		return nil, nil
	}
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}
	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}
	switch t := typed.(type) {
	case *credv1.DirectCredentials:
		return directToHelmHTTPCredentials(t.Properties), nil
	case *HelmHTTPCredentials:
		return t, nil
	}
	return nil, fmt.Errorf("unsupported credential type for helm HTTP transport: %v", typed.GetType())
}

// ConvertToOCICredentials converts runtime.Typed credentials into *ocicredsv1.OCICredentials.
// DirectCredentials are mapped using the OCI-relevant fields (username, password, accessToken, refreshToken).
// Returns nil, nil for nil input or input with an empty type.
func ConvertToOCICredentials(creds runtime.Typed) (*ocicredsv1.OCICredentials, error) {
	if creds == nil || creds.GetType().String() == "" {
		return nil, nil
	}
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}
	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}
	switch t := typed.(type) {
	case *credv1.DirectCredentials:
		return directToOCICredentials(t.Properties), nil
	case *ocicredsv1.OCICredentials:
		return t, nil
	}
	return nil, fmt.Errorf("unsupported credential type for OCI transport: %v", typed.GetType())
}
