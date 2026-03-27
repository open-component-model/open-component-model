package access

import (
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
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
