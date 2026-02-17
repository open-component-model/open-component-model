package v2

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	obj := &LocalBlob{}
	scheme.MustRegisterWithAlias(obj,
		runtime.NewVersionedType(LocalBlobAccessType, LocalBlobAccessTypeVersion),
		runtime.NewUnversionedType(LocalBlobAccessType),
	)
}
