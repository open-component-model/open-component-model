package access

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	LegacyHelmChartConsumerType = "HelmChartRepository"
	HelmRepositoryType          = "helmChart"
)

// CredentialConsumerIdentity resolves the credential consumer identity for the
// given helm repository URL. For OCI-based repositories (oci:// scheme) the
// identity type is OCIRegistry; for HTTP/HTTPS repositories it is
// HelmChartRepository. Returns nil if helmRepository is empty (local chart).
func CredentialConsumerIdentity(helmRepository string) (runtime.Identity, error) {
	if helmRepository == "" {
		return nil, nil
	}

	identity, err := runtime.ParseURLToIdentity(helmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ocicredentialsspecv1.Type)
	} else {
		identity.SetType(runtime.NewUnversionedType(LegacyHelmChartConsumerType))
	}

	return identity, nil
}

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	helm := &v1.Helm{}
	scheme.MustRegisterWithAlias(helm,
		runtime.NewVersionedType("Helm", v1.Version),
		runtime.NewUnversionedType("Helm"),
		runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion),
		runtime.NewUnversionedType(v1.LegacyType),
	)
}
