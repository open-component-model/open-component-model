package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var GetMavenArtifactV1alpha1 = runtime.NewVersionedType(GetMavenArtifactType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&GetMavenArtifact{}, GetMavenArtifactV1alpha1)
}
