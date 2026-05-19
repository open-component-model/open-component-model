package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCICredentialsType is the type name for OCI registry credentials.
const OCICredentialsType = "OCICredentials"

var OCICredentialsVersionedType = runtime.NewVersionedType(OCICredentialsType, Version)

// OCICredentials represents typed credentials for OCI registry authentication.
// It supports username/password and token-based authentication flows used by
// container registries that implement the OCI distribution specification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCICredentials struct {
	// +ocm:jsonschema-gen:enum=OCICredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCICredentials
	Type         runtime.Type `json:"type"`
	Username     string       `json:"username,omitempty"`
	Password     string       `json:"password,omitempty"`
	AccessToken  string       `json:"accessToken,omitempty"`
	RefreshToken string       `json:"refreshToken,omitempty"`
}

// FromDirectCredentials converts a DirectCredentials properties map into typed OCICredentials.
// This supports old .ocmconfig files that use Credentials/v1 with OCI registry properties.
func FromDirectCredentials(properties map[string]string) *OCICredentials {
	return &OCICredentials{
		Type:         runtime.NewVersionedType(OCICredentialsType, Version),
		Username:     properties[CredentialKeyUsername],
		Password:     properties[CredentialKeyPassword],
		AccessToken:  properties[CredentialKeyAccessToken],
		RefreshToken: properties[CredentialKeyRefreshToken],
	}
}

// FromTyped converts runtime.Typed into OCICredentials.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func FromTyped(creds runtime.Typed) (*OCICredentials, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *OCICredentials:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		ociCredentials := OCICredentials{}
		if err := Scheme.Convert(creds, &ociCredentials); err != nil {
			return nil, fmt.Errorf("error converting raw credentials to OCICredentials: %w", err)
		}
		return &ociCredentials, nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		ociCredentials := OCICredentials{}
		if err := json.Unmarshal(data, &ociCredentials); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to OCICredentials: %w", err)
		}
		return &ociCredentials, nil
	}

	slog.Error("unexpected credential type, expected OCICredentials or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
