package dummytype

import (
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&dummyv1.Repository{},
		runtime.NewVersionedType(dummyv1.Type, dummyv1.Version),
		runtime.NewUnversionedType(dummyv1.Type),
		runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version),
		runtime.NewUnversionedType(dummyv1.ShortType),
	)
}
