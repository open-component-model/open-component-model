package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OIDCIdentityTokenType = "OIDCIdentityToken"
	Version               = "v1"
)

const (
	CredentialKeyToken     = "token"
	CredentialKeyTokenFile = "token_file"
)

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
// A nil map is safe and returns an OIDCIdentityToken with only the type set.
func FromDirectCredentials(properties map[string]string) *OIDCIdentityToken {
	return &OIDCIdentityToken{
		Type:      runtime.NewVersionedType(OIDCIdentityTokenType, Version),
		Token:     properties[CredentialKeyToken],
		TokenFile: properties[CredentialKeyTokenFile],
	}
}
