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
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewVersionedType("OCIImageLayer", v2.Version))

	ociArtifact := &v2.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType("OCIImage", v2.Version))
}

func MustAddLegacyToScheme(scheme *runtime.Scheme) {
	ociImageLayer := &v2.OCIImageLayer{}
	scheme.MustRegisterWithAlias(ociImageLayer, runtime.NewVersionedType(v2.LegacyOCIBlobAccessType, v2.LegacyOCIBlobAccessTypeVersion))
	ociArtifact := &v2.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType, v2.LegacyTypeVersion))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType2, v2.LegacyType2Version))
	scheme.MustRegisterWithAlias(ociArtifact, runtime.NewVersionedType(v2.LegacyType3, v2.LegacyType3Version))
}
