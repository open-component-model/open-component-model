package spec

import (
	v1 "ocm.software/open-component-model/bindings/go/constructor/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.Helm{}, runtime.NewVersionedType("helmChart", v1.Version))
	scheme.MustRegisterWithAlias(&v1.Helm{}, runtime.NewUnversionedType("helmChart"))
}
