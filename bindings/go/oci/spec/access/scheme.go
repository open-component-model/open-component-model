package oci

import (
	v2 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	ociImageLayer := &v2.OCIImageLayer{}
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewVersionedType("OCIImageLayer", v2.Version),
		runtime.NewUnversionedType("OCIImageLayer"))

	ociArtifact := &v2.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType("OCIImage", v2.Version),
		runtime.NewUnversionedType("OCIImage"))
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociImageLayer := &v2.OCIImageLayer{}
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewVersionedType(v2.LegacyOCIBlobAccessType, v2.LegacyOCIBlobAccessTypeVersion),
		runtime.NewUnversionedType(v2.LegacyOCIBlobAccessType))
	ociArtifact := &v2.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType, v2.LegacyTypeVersion),
		runtime.NewUnversionedType(v2.LegacyType))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType2, v2.LegacyType2Version),
		runtime.NewUnversionedType(v2.LegacyType2))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType3, v2.LegacyType3Version),
		runtime.NewUnversionedType(v2.LegacyType3))
}
