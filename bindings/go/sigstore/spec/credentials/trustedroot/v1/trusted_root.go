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
	TrustedRootType = "TrustedRoot"
	Version         = "v1"
)

const (
	CredentialKeyTrustedRootJSON     = "trusted_root_json"
	CredentialKeyTrustedRootJSONFile = "trusted_root_json_file"
)

// TrustedRoot represents typed credentials for Sigstore verification trust material.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TrustedRoot struct {
	// +ocm:jsonschema-gen:enum=TrustedRoot/v1
	// +ocm:jsonschema-gen:enum:deprecated=TrustedRoot
	Type                runtime.Type `json:"type"`
	TrustedRootJSON     string       `json:"trustedRootJSON,omitempty"`
	TrustedRootJSONFile string       `json:"trustedRootJSONFile,omitempty"`
}

func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&TrustedRoot{},
		runtime.NewVersionedType(TrustedRootType, Version),
		runtime.NewUnversionedType(TrustedRootType),
	)
}

func FromDirectCredentials(properties map[string]string) *TrustedRoot {
	return &TrustedRoot{
		Type:                runtime.NewVersionedType(TrustedRootType, Version),
		TrustedRootJSON:     properties[CredentialKeyTrustedRootJSON],
		TrustedRootJSONFile: properties[CredentialKeyTrustedRootJSONFile],
	}
}

// FromTyped converts runtime.Typed into TrustedRoot.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func FromTyped(creds runtime.Typed) (*TrustedRoot, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *TrustedRoot:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		TrustedRoot := TrustedRoot{}
		if err := Scheme.Convert(creds, &TrustedRoot); err != nil {
			return nil, fmt.Errorf("error converting raw credentials to TrustedRoot: %w", err)
		}
		return &TrustedRoot, nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		TrustedRoot := TrustedRoot{}
		if err := json.Unmarshal(data, &TrustedRoot); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to TrustedRoot: %w", err)
		}
		return &TrustedRoot, nil
	}

	slog.Error("unexpected credential type, expected TrustedRoot or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
