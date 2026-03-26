package internal

import (
	"context"
	"fmt"
	"log/slog"

	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TODO(fabianburth): digest processor and access should both be implemented on
//
//	a helm repository (https://github.com/open-component-model/ocm-project/issues/911).
//	This logic should move into the helm repository package and should rather
//	be implemented as a unexported method on the helm repository.
func GetIdentity(ctx context.Context, obj runtime.Typed) (runtime.Identity, error) {
	switch access := obj.(type) {
	case *v1.Helm:
		if access.HelmRepository == "" {
			slog.InfoContext(ctx, "local helm inputs do not require credentials")
			return nil, nil
		}

		identity, err := runtime.ParseURLToIdentity(access.HelmRepository)
		if err != nil {
			return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
		}

		if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
			identity.SetType(ocicredentialsspecv1.Type)
		} else {
			identity.SetType(runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType))
		}

		return identity, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for getting identity", obj.GetType())
	}
}
