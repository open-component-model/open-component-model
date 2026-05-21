package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// SigstoreCredentialsType is the type name for Sigstore keyless signing credentials.
	//nolint:gosec // G101: This is a type name, not a credential.
	SigstoreCredentialsType = "SigstoreCredentials"
	// Version is the version of the SigstoreCredentials type.
	Version = "v1alpha1"
)

var SigstoreCredentialsVersionedType = runtime.NewVersionedType(SigstoreCredentialsType, Version)

// SigstoreCredentials holds credentials for Sigstore keyless signing and verification.
//
// The OIDC identity token identifies the signer to Fulcio, which issues a short-lived
// certificate. Provide either Token (inline) or TokenFile (path); Token takes precedence.
// At least one must be set for signing unless the SIGSTORE_ID_TOKEN environment variable
// or GitHub Actions ambient OIDC is available.
//
// The trusted root overrides the default public-good Sigstore TUF root and is required
// when signing or verifying against private Sigstore infrastructure. Provide either
// TrustedRootJSON (inline) or TrustedRootJSONFile (path); TrustedRootJSON takes precedence.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SigstoreCredentials struct {
	// +ocm:jsonschema-gen:enum=SigstoreCredentials/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreCredentials
	Type runtime.Type `json:"type"`
	// Token is an inline OIDC identity token used to authenticate to Fulcio during signing.
	// Takes precedence over TokenFile when both are set.
	Token string `json:"token,omitempty"`
	// TokenFile is a path to a file containing an OIDC identity token.
	// Ignored when Token is also set.
	TokenFile string `json:"tokenFile,omitempty"`
	// TrustedRootJSON is an inline JSON document conforming to the Sigstore TrustedRoot
	// schema. Overrides the default public-good TUF root for private Sigstore infrastructure.
	// Takes precedence over TrustedRootJSONFile when both are set.
	TrustedRootJSON string `json:"trustedRootJSON,omitempty"`
	// TrustedRootJSONFile is a path to a JSON file conforming to the Sigstore TrustedRoot schema.
	// Same semantics as TrustedRootJSON, but loaded from disk. Ignored when TrustedRootJSON is also set.
	// Must be an absolute, canonical path (no .. segments).
	TrustedRootJSONFile string `json:"trustedRootJSONFile,omitempty"`
}
