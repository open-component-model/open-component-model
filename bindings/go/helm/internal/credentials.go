package internal

import (
	"fmt"

	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialConsumerIdentity resolves the credential consumer identity for the
// given helm repository URL. For OCI-based repositories (oci:// scheme) the
// identity type is OCIRegistry; for HTTP/HTTPS repositories it is
// HelmChartRepository. Returns an error if helmRepository is empty (e.g. for local charts).
func CredentialConsumerIdentity(helmRepository string) (runtime.Typed, error) {
	if helmRepository == "" {
		return nil, fmt.Errorf("no helm repository specified")
	}

	identity, err := runtime.ParseURLToIdentity(helmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ociidentityv1.Type)
	} else {
		identity.SetType(helmidentityv1.Type)
	}

	return identity, nil
}

// CredentialConsumerIdentityCompat resolves the credential consumer identity for the
// given helm repository URL and returns runtime.Identity for compatibility.
// For OCI-based repositories (oci:// scheme) the identity type is OCIRegistry; for HTTP/HTTPS repositories it is
// HelmChartRepository. Returns an error if helmRepository is empty (e.g. for local charts).
func CredentialConsumerIdentityCompat(helmRepository string) (runtime.Identity, error) {
	identity, err := CredentialConsumerIdentity(helmRepository)
	if err != nil {
		return nil, fmt.Errorf("error getting credential identity: %w", err)
	}

	compatIdentity, ok := identity.(runtime.Identity)
	if !ok {
		// this is for now a developer error and needs to crash to prevent wrong introductions of non map identities
		// TODO(matthiasbruns): https://github.com/open-component-model/ocm-project/issues/1041
		panic(fmt.Sprintf("unexpected type for credential consumer identity: %T", identity))
	}

	return compatIdentity, nil
}
