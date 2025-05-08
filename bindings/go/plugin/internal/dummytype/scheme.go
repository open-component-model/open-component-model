package dummytype

import (
	v2 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v2.Repository{},
		runtime.NewVersionedType(v2.Type, v2.Version),
		runtime.NewUnversionedType(v2.Type),
		runtime.NewVersionedType(v2.ShortType, v2.Version),
		runtime.NewUnversionedType(v2.ShortType),
	)
}
