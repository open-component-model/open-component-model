package access

import (
	"context"
	"errors"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const LegacyHelmChartConsumerType = "HelmChartRepository"

// ErrLocalHelmInputDoesNotRequireCredentials is returned when credential-related operations are attempted
// on local helm inputs, since those are based on local filesystem and do not require authentication or authorization.
var ErrLocalHelmInputDoesNotRequireCredentials = errors.New("local helm inputs do not require credentials")

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
func (h *HelmAccess) GetResourceCredentialConsumerIdentity(_ context.Context, resource *descruntime.Resource) (identity runtime.Identity, err error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	helm := v1.Helm{}
	if err := Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if helm.HelmRepository == "" {
		return nil, ErrLocalHelmInputDoesNotRequireCredentials
	}

	identity, err = runtime.ParseURLToIdentity(helm.HelmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	identity.SetType(runtime.NewUnversionedType(LegacyHelmChartConsumerType))

	return identity, nil
}
