package repository

import (
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	ociRepository := &v1.OCIRepository{}
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.Type, v1.Version))
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociRepository := &v1.OCIRepository{}
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.LegacyRegistryType, v1.Version))
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.LegacyRegistryType2, v1.Version))
}
