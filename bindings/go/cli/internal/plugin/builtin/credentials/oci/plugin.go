package oci

import (
	"fmt"

	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ociidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	registry *credentialrepository.RepositoryRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
) error {
	if err := credentialTypeRegistry.Register(ocicredentialsspec.CredentialTypeScheme); err != nil {
		return fmt.Errorf("could not register OCI credential types: %w", err)
	}

	return registry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]runtime.Type{ociidentity.Type},
	)
}
