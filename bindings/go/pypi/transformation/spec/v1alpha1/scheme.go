package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var GetPyPIArtifactV1alpha1 = runtime.NewVersionedType(GetPyPIArtifactType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&GetPyPIArtifact{}, GetPyPIArtifactV1alpha1)
}
