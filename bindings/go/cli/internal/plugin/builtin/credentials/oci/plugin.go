package oci

import (
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ociidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(registry *credentialrepository.RepositoryRegistry) error {
	return registry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]runtime.Type{ociidentity.Type},
	)
}
