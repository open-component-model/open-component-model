package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	dockerConfig := &DockerConfig{}
	scheme.MustRegisterWithAlias(dockerConfig,
		runtime.NewVersionedType(DockerConfigType, Version),
		runtime.NewUnversionedType(DockerConfigType),
	)

	ociCredentials := &OCICredentials{}
	scheme.MustRegisterWithAlias(ociCredentials,
		runtime.NewVersionedType(OCICredentialsType, Version),
		runtime.NewUnversionedType(OCICredentialsType),
	)
}
