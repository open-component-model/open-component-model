package oidcidentitytoken

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
)

// Scheme is the [runtime.Scheme] holding the OIDCIdentityToken credential type.
// External consumers (for example a credential graph that needs to resolve
// SigstoreSigner consumer identities to a concrete credential) can import this
// scheme to register or look up the type.
var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers OIDCIdentityToken/v1alpha1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1alpha1.OIDCIdentityToken{},
		v1alpha1.VersionedType,
		runtime.NewUnversionedType(v1alpha1.OIDCIdentityTokenType),
	)
}
