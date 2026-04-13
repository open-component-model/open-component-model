package access

import (
	v2 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
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
	helm := &v2.Helm{}
	scheme.MustRegisterWithAlias(helm,
		runtime.NewVersionedType("Helm", v2.Version),
		runtime.NewUnversionedType("Helm"),
		runtime.NewVersionedType(v2.LegacyType, v2.LegacyTypeVersion),
		runtime.NewUnversionedType(v2.LegacyType),
	)
}
