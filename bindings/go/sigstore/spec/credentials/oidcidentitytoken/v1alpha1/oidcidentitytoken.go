package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// OIDCIdentityTokenType is the type name for OIDC identity token credentials
	// used by the sigstore signing handler to authenticate to Fulcio.
	OIDCIdentityTokenType = "OIDCIdentityToken"
	// Version is the version of the OIDCIdentityToken type.
	Version = "v1alpha1"
)

// VersionedType is the canonical versioned [runtime.Type] for OIDCIdentityToken credentials.
var VersionedType = runtime.NewVersionedType(OIDCIdentityTokenType, Version)

// OIDCIdentityToken carries an OIDC identity token used by the sigstore signing handler
// to authenticate the signer to Fulcio. Fulcio issues a short-lived signing certificate
// bound to the identity claims in the token.
//
// Provide either Token (inline) or TokenFile (path); Token takes precedence when both
// are set. At least one must be set for signing unless the SIGSTORE_ID_TOKEN environment
// variable or GitHub Actions ambient OIDC is available.
//
// This credential is consumed by the sigstore signing handler's Sign path. It is not
// consumed by the Verify path; verification uses [TrustedRoot] instead.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OIDCIdentityToken struct {
	// +ocm:jsonschema-gen:enum=OIDCIdentityToken/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=OIDCIdentityToken
	Type runtime.Type `json:"type"`
	// Token is an inline OIDC identity token forwarded to cosign as SIGSTORE_ID_TOKEN for
	// Fulcio authentication during keyless signing. Required when neither SIGSTORE_ID_TOKEN
	// nor ACTIONS_ID_TOKEN_REQUEST_TOKEN is already set in the process environment.
	// Takes precedence over TokenFile when both are set.
	Token string `json:"token,omitempty"`
	// TokenFile is a path to a file containing an OIDC identity token.
	// Same semantics as Token, but read from disk. Ignored when Token is also set.
	TokenFile string `json:"tokenFile,omitempty"`
}
