package credentials

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	CredentialRepositoryConfigType = runtime.NewVersionedType("DockerConfig", "v1")
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.DockerConfig{}, CredentialRepositoryConfigType)
}
