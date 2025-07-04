package oci

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(registry *credentialrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ocicredentialsspecv1.DockerConfig{}, ocicredentialsspec.CredentialRepositoryConfigType)
	return credentialrepository.RegisterInternalCredentialRepositoryPlugin(
		scheme,
		registry,
		&ocicredentials.OCICredentialRepository{},
		&ocicredentialsspecv1.DockerConfig{},
		[]runtime.Type{credentials.AnyConsumerIdentityType},
	)
}
