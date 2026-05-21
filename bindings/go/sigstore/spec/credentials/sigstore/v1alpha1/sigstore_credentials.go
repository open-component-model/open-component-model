package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// OIDCIdentityTokenType is the type name for Sigstore keyless signing credentials.
	//nolint:gosec // G101: This is a type name, not a credential.
	OIDCIdentityTokenType = "OIDCIdentityToken"
	// Version is the version of the OIDCIdentityToken type.
	Version = "v1alpha1"
)

var OIDCIdentityTokenVersionedType = runtime.NewVersionedType(OIDCIdentityTokenType, Version)

// OIDCIdentityToken holds credentials for Sigstore keyless signing and verification.
//
// The OIDC identity token identifies the signer to Fulcio, which issues a short-lived
// certificate. Provide either Token (inline) or TokenFile (path); Token takes precedence.
// At least one must be set for signing unless the SIGSTORE_ID_TOKEN environment variable
// or GitHub Actions ambient OIDC is available.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OIDCIdentityToken struct {
	// +ocm:jsonschema-gen:enum=OIDCIdentityToken/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=OIDCIdentityToken
	Type runtime.Type `json:"type"`
	// Token is an inline OIDC identity token used to authenticate to Fulcio during signing.
	// Takes precedence over TokenFile when both are set.
	Token string `json:"token,omitempty"`
	// TokenFile is a path to a file containing an OIDC identity token.
	// Ignored when Token is also set.
	TokenFile string `json:"tokenFile,omitempty"`
}
