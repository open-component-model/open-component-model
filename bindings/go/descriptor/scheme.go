package descriptor

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	obj := &LocalBlob{}
	scheme.MustRegisterWithAlias(obj, runtime.NewUngroupedVersionedType(LocalBlobAccessType, LocalBlobAccessTypeV1))
}
