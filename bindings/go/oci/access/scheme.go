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
	ociImageLayer := &v1.OCIImageLayer{}
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewType(v1.Group, v1.Version, "OCIImageLayer"))

	ociArtifact := &v1.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewType(v1.Group, v1.Version, "OCIImage"))
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociImageLayer := &v1.OCIImageLayer{}
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewUngroupedVersionedType(v1.LegacyOCIBlobAccessType, v1.LegacyOCIBlobAccessTypeVersion))
	ociArtifact := &v1.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewUngroupedVersionedType(v1.LegacyType, v1.LegacyTypeVersion))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewUngroupedVersionedType(v1.LegacyType2, v1.LegacyType2Version))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewUngroupedVersionedType(v1.LegacyType3, v1.LegacyType3Version))
}
