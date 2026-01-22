package access

import (
	"ocm.software/open-component-model/bindings/go/blob/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	file := &v1alpha1.File{}
	scheme.MustRegisterWithAlias(file,
		runtime.NewVersionedType(v1alpha1.FileType, v1alpha1.Version),
		runtime.NewUnversionedType(v1alpha1.FileType),
		runtime.NewVersionedType(v1alpha1.LegacyFileType, v1alpha1.Version),
		runtime.NewUnversionedType(v1alpha1.LegacyFileType),
	)
}
