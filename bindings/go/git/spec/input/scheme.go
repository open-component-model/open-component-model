package input

import (
	v1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var V1VersionedType = runtime.NewVersionedType(v1.Type, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.Git{},
		V1VersionedType,
		runtime.NewUnversionedType(v1.Type),
		runtime.NewUnversionedType("Git"),
		runtime.NewVersionedType("Git", v1.Version),
	)
}
