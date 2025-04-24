package oci

import (
	v2 "ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	v2alpha2 "ocm.software/open-component-model/bindings/go/constructor/input/utf8/spec/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v2.File{}, runtime.NewVersionedType("file", v2.Version))
	scheme.MustRegisterWithAlias(&v2.File{}, runtime.NewUnversionedType("file"))

	scheme.MustRegisterWithAlias(&v2alpha2.UTF8{}, runtime.NewVersionedType("utf8", v2alpha2.Version))
	scheme.MustRegisterWithAlias(&v2alpha2.UTF8{}, runtime.NewUnversionedType("utf8"))
}
