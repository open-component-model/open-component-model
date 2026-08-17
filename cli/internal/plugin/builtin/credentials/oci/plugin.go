package oci

import (
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ociidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(registry *credentialrepository.RepositoryRegistry, credTypeRegistry *credentialtyperepository.CredentialTypeRegistry) error {
	registry.Register(ocicredentialsspec.CredentialTypeScheme)
	// TODO(matthiasbruns): register
	credTypeRegistry.RegisterInternalCredentialTypeSchemeProvider()

	return registry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]runtime.Type{ociidentity.Type},
	)
}
