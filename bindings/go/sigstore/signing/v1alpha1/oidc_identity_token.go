package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// IdentityTypeOIDCIdentityToken is the credential consumer identity type for Sigstore
// keyless signing. It signals that the handler needs an OIDC identity token to authenticate
// with Fulcio.
var IdentityTypeOIDCIdentityToken = runtime.NewVersionedType("OIDCIdentityToken", Version)

// OIDCIdentityToken represents a resolved OIDC identity token credential
// for Sigstore keyless signing.
type OIDCIdentityToken struct {
	// Type identifies this credential type.
	Type runtime.Type `json:"type"`

	// Token is the OIDC identity token value.
	Token string `json:"token"`
}
