package oci

import (
	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	obj := &v1.OCIImageLayer{}
	scheme.MustRegisterWithAlias(v1.OCIImageLayer{}, runtime.NewType(v1.Group, v1.Version, "OCIImageLayer"))
	scheme.MustRegisterWithAlias(obj, runtime.NewUngroupedVersionedType(v1.LegacyOCIBlobAccessType, v1.LegacyOCIBlobAccessTypeVersion))
}
