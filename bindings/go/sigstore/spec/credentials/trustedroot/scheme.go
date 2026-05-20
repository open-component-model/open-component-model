package trustedroot

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	trustedRoot := &v1.TrustedRoot{}
	scheme.MustRegisterWithAlias(trustedRoot,
		v1.TrustedRootVersionedType,
		runtime.NewUnversionedType(v1.TrustedRootType),
	)
}
