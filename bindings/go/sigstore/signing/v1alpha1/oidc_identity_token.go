package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// CredentialTypeOIDCIdentityToken is the credential type for OIDC identity tokens
// used in Sigstore keyless signing.
var CredentialTypeOIDCIdentityToken = runtime.NewVersionedType("OIDCIdentityToken", Version)

// OIDCIdentityToken represents a resolved OIDC identity token credential
// for Sigstore keyless signing.
//
// TODO(ocm-project#702): Align with the typed credential system (ADR 0018). Once Phase 2
// lands, this struct should become a proper runtime.Typed with scheme registration,
// Validate(), and generated deepcopy.
type OIDCIdentityToken struct {
	// Type identifies this credential type.
	Type runtime.Type `json:"type"`

	// Token is the OIDC identity token value.
	Token string `json:"token"`
}
