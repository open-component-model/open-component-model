package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OIDCIdentityTokenType = "OIDCIdentityToken"
	Version               = "v1"
)

const (
	CredentialKeyToken     = "token"
	CredentialKeyTokenFile = "tokenFile"
)

// Deprecated: Use CredentialKeyTokenFile instead.
const DeprecatedCredentialKeyTokenFile = "token_file"

// OIDCIdentityToken represents typed credentials for Sigstore keyless signing.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OIDCIdentityToken struct {
	// +ocm:jsonschema-gen:enum=OIDCIdentityToken/v1
	// +ocm:jsonschema-gen:enum:deprecated=OIDCIdentityToken
	Type      runtime.Type `json:"type"`
	Token     string       `json:"token,omitempty"`
	TokenFile string       `json:"tokenFile,omitempty"`
}

// MustRegisterCredentialType registers OIDCIdentityToken/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&OIDCIdentityToken{},
		runtime.NewVersionedType(OIDCIdentityTokenType, Version),
		runtime.NewUnversionedType(OIDCIdentityTokenType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed OIDCIdentityToken.
// Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns an OIDCIdentityToken with only the type set.
// FromDirectCredentials converts a DirectCredentials properties map into typed OIDCIdentityToken.
// Both camelCase and deprecated snake_case keys are accepted.
// A nil map is safe and returns an OIDCIdentityToken with only the type set.
func FromDirectCredentials(properties map[string]string) *OIDCIdentityToken {
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

// FromTyped converts runtime.Typed into OIDCIdentityToken.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func FromTyped(creds runtime.Typed) (*OIDCIdentityToken, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *OIDCIdentityToken:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		OIDCIdentityToken := OIDCIdentityToken{}
		if err := Scheme.Convert(creds, &OIDCIdentityToken); err != nil {
			return nil, fmt.Errorf("error converting raw credentials to OIDCIdentityToken: %w", err)
		}
		return &OIDCIdentityToken, nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		OIDCIdentityToken := OIDCIdentityToken{}
		if err := json.Unmarshal(data, &OIDCIdentityToken); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to OIDCIdentityToken: %w", err)
		}
		return &OIDCIdentityToken, nil
	}

	slog.Error("unexpected credential type, expected OIDCIdentityToken or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
