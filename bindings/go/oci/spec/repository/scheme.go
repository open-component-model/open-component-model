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
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.ShortType, v1.Version))
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewUnversionedType(v1.ShortType))

	ctfRepository := &v1.CTFRepository{}
	scheme.MustRegisterWithAlias(ctfRepository, runtime.NewVersionedType(v1.TypeCTF, v1.Version))
	scheme.MustRegisterWithAlias(ctfRepository, runtime.NewVersionedType(v1.ShortTypeCTF, v1.Version))
	scheme.MustRegisterWithAlias(ctfRepository, runtime.NewUnversionedType(v1.ShortTypeCTF))
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociRepository := &v1.OCIRepository{}
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.LegacyRegistryType, v1.Version))
	scheme.MustRegisterWithAlias(ociRepository, runtime.NewVersionedType(v1.LegacyRegistryType2, v1.Version))
}
