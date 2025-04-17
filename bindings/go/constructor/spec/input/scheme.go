package oci

import (
	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/constructor/spec/input/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.File{}, runtime.NewVersionedType("file", v1.Version))
	scheme.MustRegisterWithAlias(&v1.File{}, runtime.NewUnversionedType("file"))

	scheme.MustRegisterWithAlias(&v2alpha1.UTF8{}, runtime.NewVersionedType("utf8", v2alpha1.Version))
	scheme.MustRegisterWithAlias(&v2alpha1.UTF8{}, runtime.NewUnversionedType("utf8"))
}
