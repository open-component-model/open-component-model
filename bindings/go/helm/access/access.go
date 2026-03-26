package access

import (
	"context"
	"fmt"
	"log/slog"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	LegacyHelmChartConsumerType = "HelmChartRepository"
	HelmRepositoryType          = "helmChart"
)

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

// HelmAccess is a ResourceCredentialConsumerIdentityProvider that provides consumer identities for Helm repository resources.
// TODO(matthiasbruns): Introduce a helm based ResourceRepository and move this logic into the implementation. https://github.com/open-component-model/ocm-project/issues/911
type HelmAccess struct{}

// GetResourceCredentialConsumerIdentity returns the consumer identity for the given resource if the resource access is a Helm repository.
func (h *HelmAccess) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descruntime.Resource) (identity runtime.Identity, err error) {
	helm := v1.Helm{}
	if err := Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if helm.HelmRepository == "" {
		slog.DebugContext(ctx, "local helm inputs do not require credentials")
		return nil, nil
	}

	identity, err = runtime.ParseURLToIdentity(helm.HelmRepository)
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
