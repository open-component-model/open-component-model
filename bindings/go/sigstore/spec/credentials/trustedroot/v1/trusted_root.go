package v1

import (
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
