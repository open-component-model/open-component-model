package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var GetWgetV1alpha1 = runtime.NewVersionedType(GetWgetType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&GetWget{}, GetWgetV1alpha1)
}
