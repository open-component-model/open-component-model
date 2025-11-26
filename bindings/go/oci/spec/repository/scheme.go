package repository

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&oci.Repository{},
		runtime.NewVersionedType(oci.Type, oci.Version),
		runtime.NewUnversionedType(oci.Type),
		runtime.NewVersionedType(oci.ShortType, oci.Version),
		runtime.NewUnversionedType(oci.ShortType),
		runtime.NewVersionedType(oci.ShortType2, oci.Version),
		runtime.NewUnversionedType(oci.ShortType2),
	)

	scheme.MustRegisterWithAlias(&ctf.Repository{},
		runtime.NewVersionedType(ctf.Type, ctf.Version),
		runtime.NewUnversionedType(ctf.Type),
		runtime.NewVersionedType(ctf.ShortType, ctf.Version),
		runtime.NewUnversionedType(ctf.ShortType),
		runtime.NewVersionedType(ctf.ShortType2, ctf.Version),
		runtime.NewUnversionedType(ctf.ShortType2),
	)
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociRepository := &oci.Repository{}
	scheme.MustRegisterWithAlias(ociRepository,
		runtime.NewVersionedType(oci.LegacyRegistryType, oci.Version),
		runtime.NewUnversionedType(oci.LegacyRegistryType),
	)
	scheme.MustRegisterWithAlias(ociRepository,
		runtime.NewVersionedType(oci.LegacyRegistryType2, oci.Version),
		runtime.NewUnversionedType(oci.LegacyRegistryType2),
	)
}
