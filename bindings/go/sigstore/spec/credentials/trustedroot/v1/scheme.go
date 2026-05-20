package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	trustedRoot := &TrustedRoot{}
	scheme.MustRegisterWithAlias(trustedRoot,
		runtime.NewVersionedType(TrustedRootType, Version),
		runtime.NewUnversionedType(TrustedRootType),
	)
}
