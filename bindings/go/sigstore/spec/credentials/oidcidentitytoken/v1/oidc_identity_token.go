package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OIDCIdentityTokenType = "OIDCIdentityToken"
	Version               = "v1"
)

var OIDCIdentityTokenVersionedType = runtime.NewVersionedType(OIDCIdentityTokenType, Version)

// OIDCIdentityToken represents typed credentials for Sigstore keyless signing.
// Optionally verification trust material is established by providing TrustedRootJSON or TrustedRootJSONFile.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OIDCIdentityToken struct {
	// +ocm:jsonschema-gen:enum=OIDCIdentityToken/v1
	// +ocm:jsonschema-gen:enum:deprecated=OIDCIdentityToken
	Type                runtime.Type `json:"type"`
	Token               string       `json:"token,omitempty"`
	TokenFile           string       `json:"tokenFile,omitempty"`
	TrustedRootJSON     string       `json:"trustedRootJSON,omitempty"`
	TrustedRootJSONFile string       `json:"trustedRootJSONFile,omitempty"`
}
