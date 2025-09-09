package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&PSSConfig{},
		runtime.NewUnversionedType(AlgorithmRSASSAPSS),
		runtime.NewVersionedType(AlgorithmRSASSAPSS, Version),
	)
	Scheme.MustRegisterWithAlias(&PKCS1V15Config{},
		runtime.NewUnversionedType(AlgorithmRSASSAPKCS1V15),
		runtime.NewVersionedType(AlgorithmRSASSAPKCS1V15, Version),
	)
}
