package trustedroot

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1alpha1"
)

// Scheme is the [runtime.Scheme] holding the TrustedRoot credential type.
// External consumers (for example a credential graph that needs to resolve
// SigstoreVerifier consumer identities to a concrete credential) can import this
// scheme to register or look up the type.
var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers TrustedRoot/v1alpha1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1alpha1.TrustedRoot{},
		v1alpha1.VersionedType,
		runtime.NewUnversionedType(v1alpha1.TrustedRootType),
	)
}
