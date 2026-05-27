package credentials

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var CredentialRepositoryConfigType = runtime.NewVersionedType("DockerConfig", "v1")

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	dockerConfig := &v1.DockerConfig{}
	scheme.MustRegisterWithAlias(dockerConfig,
		CredentialRepositoryConfigType,
		runtime.NewUnversionedType(v1.DockerConfigType),
	)

	ociCredentials := &v1.OCICredentials{}
	scheme.MustRegisterWithAlias(ociCredentials,
		runtime.NewVersionedType(v1.OCICredentialsType, v1.Version),
		runtime.NewUnversionedType(v1.OCICredentialsType),
	)
}
