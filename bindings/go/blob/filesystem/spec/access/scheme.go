package access

import (
	v1alpha2 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	file := &v1alpha2.File{}
	scheme.MustRegisterWithAlias(file,
		runtime.NewVersionedType(v1alpha2.FileType, v1alpha2.Version),
		runtime.NewUnversionedType(v1alpha2.FileType),
		runtime.NewVersionedType(v1alpha2.LegacyFileType, v1alpha2.Version),
		runtime.NewUnversionedType(v1alpha2.LegacyFileType),
	)
}
