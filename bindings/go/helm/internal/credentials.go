package internal

import (
	"fmt"

	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/identity/v1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialConsumerIdentity resolves the credential consumer identity for the
// given helm repository URL. For OCI-based repositories (oci:// scheme) the
// identity type is OCIRegistry; for HTTP/HTTPS repositories it is
// HelmChartRepository. Returns an error if helmRepository is empty (e.g. for local charts).
func CredentialConsumerIdentity(helmRepository string) (runtime.Identity, error) {
	if helmRepository == "" {
		return nil, fmt.Errorf("no helm repository specified")
	}

	identity, err := runtime.ParseURLToIdentity(helmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ocicredentialsspecv1.Type)
	} else {
		identity.SetType(helmidentityv1.Type)
	}

	return identity, nil
}
