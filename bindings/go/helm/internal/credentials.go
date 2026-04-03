package internal

import (
	"errors"
	"fmt"

	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrLocalHelmInputDoesNotRequireCredentials is returned when credential-related operations are attempted
// on local helm inputs, since those are based on local filesystem and do not require authentication or authorization.
var ErrLocalHelmInputDoesNotRequireCredentials = errors.New("local helm inputs do not require credentials")

// CredentialConsumerIdentity resolves the credential consumer identity for the
// given helm repository URL. For OCI-based repositories (oci:// scheme) the
// identity type is OCIRegistry; for HTTP/HTTPS repositories it is
// HelmChartRepository. Returns nil if helmRepository is empty (e.g. for local charts).
func CredentialConsumerIdentity(helmRepository string) (runtime.Identity, error) {
	if helmRepository == "" {
		return nil, ErrLocalHelmInputDoesNotRequireCredentials
	}

	identity, err := runtime.ParseURLToIdentity(helmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ocicredentialsspecv1.Type)
	} else {
		identity.SetType(runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType))
	}

	return identity, nil
}
