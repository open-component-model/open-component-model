package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	helmHTTPCreds := &HelmHTTPCredentials{}
	scheme.MustRegisterWithAlias(helmHTTPCreds,
		runtime.NewVersionedType(HelmHTTPCredentialsType, v1.Version),
		runtime.NewUnversionedType(HelmHTTPCredentialsType),
	)
}

const (
	//nolint:gosec // G101: This is a type name, not a credential.
	HelmHTTPCredentialsType = "HelmHTTPCredentials"
	Version                 = "v1"

	// CredentialKeyUsername is the key for the username in HTTP-based Helm repository credentials.
	CredentialKeyUsername = "username"

	// CredentialKeyPassword is the key for the password in HTTP-based Helm repository credentials.
	CredentialKeyPassword = "password"

	// CredentialKeyCertFile is the key for the client certificate file path in HTTP-based Helm repository credentials.
	CredentialKeyCertFile = "certFile"

	// CredentialKeyKeyFile is the key for the client key file path in HTTP-based Helm repository credentials.
	CredentialKeyKeyFile = "keyFile"

	// CredentialKeyKeyring is the key for the keyring file path in HTTP-based Helm repository credentials.
	CredentialKeyKeyring = "keyring"
)

// HelmHTTPCredentials represents typed credentials for HTTP/S-based Helm chart repositories.
// For OCI-based Helm repositories, use OCICredentials/v1 instead.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmHTTPCredentials struct {
	// +ocm:jsonschema-gen:enum=HelmHTTPCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=HelmHTTPCredentials
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
	CertFile string       `json:"certFile,omitempty"`
	KeyFile  string       `json:"keyFile,omitempty"`
	Keyring  string       `json:"keyring,omitempty"`
}

// MustRegisterCredentialType registers HelmHTTPCredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&HelmHTTPCredentials{},
		runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		runtime.NewUnversionedType(HelmHTTPCredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed HelmHTTPCredentials.
// This supports old .ocmconfig files that use Credentials/v1 with Helm HTTP properties.
// A nil map is safe and returns an empty HelmHTTPCredentials with only the type set.
func FromDirectCredentials(properties map[string]string) *HelmHTTPCredentials {
	return &HelmHTTPCredentials{
		Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		Username: properties[CredentialKeyUsername],
		Password: properties[CredentialKeyPassword],
		CertFile: properties[CredentialKeyCertFile],
		KeyFile:  properties[CredentialKeyKeyFile],
		Keyring:  properties[CredentialKeyKeyring],
	}
}

// FromTyped converts runtime.Typed into HelmHTTPCredentials.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func FromTyped(creds runtime.Typed) (*HelmHTTPCredentials, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *HelmHTTPCredentials:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		credsMap := map[string]string{}
		if err := json.Unmarshal(t.Data, &credsMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling credentials: %v", err)
		}
		return FromDirectCredentials(credsMap), nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		credsMap := map[string]string{}
		if err := json.Unmarshal(data, &credsMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling unstructured credentials: %v", err)
		}
		return FromDirectCredentials(credsMap), nil
	}

	slog.Error("unexpected credential type, expected HelmHTTPCredentials or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
