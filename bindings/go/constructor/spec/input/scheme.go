package oci

import (
	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	file := &v1.File{}
	scheme.MustRegisterWithAlias(file, runtime.NewVersionedType("file", v1.Version))
	scheme.MustRegisterWithAlias(file, runtime.NewUnversionedType("file"))
}
