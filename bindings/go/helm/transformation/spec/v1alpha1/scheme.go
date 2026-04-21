package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	GetHelmChartV1alpha1     = runtime.NewVersionedType(GetHelmChartType, Version)
	ConvertHelmToOCIV1alpha1 = runtime.NewVersionedType(ConvertHelmToOCIType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&GetHelmChart{}, GetHelmChartV1alpha1)
	Scheme.MustRegisterWithAlias(&ConvertHelmToOCI{}, ConvertHelmToOCIV1alpha1)
}
