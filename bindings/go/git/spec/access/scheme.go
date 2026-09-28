package access

import (
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.Git{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewUnversionedType(v1.LegacyType),
		runtime.NewVersionedType(v1.LegacyType, "v1alpha1"),
		runtime.NewVersionedType(v1.Type, "v1alpha1"),
	)
}
