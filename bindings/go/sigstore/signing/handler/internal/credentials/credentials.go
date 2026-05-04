package credentials

import "ocm.software/open-component-model/bindings/go/runtime"

var (
	IdentityTypeSigstoreSigner   = runtime.NewVersionedType("SigstoreSigner", "v1alpha1")
	IdentityTypeSigstoreVerifier = runtime.NewVersionedType("SigstoreVerifier", "v1alpha1")
)
