package oci

import (
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(registry *credentialrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ocicredentialsspecv1.DockerConfig{}, ocicredentialsspec.CredentialRepositoryConfigType)
	return registry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]runtime.Type{v1.Type},
	)
}
