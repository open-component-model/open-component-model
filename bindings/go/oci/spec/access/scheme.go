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
	scheme.MustRegisterWithAlias(ociImageLayer,
		runtime.NewVersionedType(v2.OCIImageLayerType, v2.Version),
		runtime.NewUnversionedType(v2.OCIImageLayerType),
		runtime.NewVersionedType(v2.LegacyOCIBlobAccessType, v2.LegacyOCIBlobAccessTypeVersion),
		runtime.NewUnversionedType(v2.LegacyOCIBlobAccessType),
	)

	ociArtifact := &v2.OCIImage{}
	scheme.MustRegisterWithAlias(ociArtifact,
		runtime.NewVersionedType(v2.OCIImageType, v2.Version),
		runtime.NewUnversionedType(v2.OCIImageType),
		runtime.NewVersionedType(v2.LegacyType, v2.LegacyTypeVersion),
		runtime.NewUnversionedType(v2.LegacyType),
		runtime.NewVersionedType(v2.LegacyType2, v2.LegacyType2Version),
		runtime.NewUnversionedType(v2.LegacyType2),
		runtime.NewVersionedType(v2.LegacyType3, v2.LegacyType3Version),
		runtime.NewUnversionedType(v2.LegacyType3),
	)

	scheme.MustRegisterWithAlias(&v2.RelativeOCIReference{},
		runtime.NewVersionedType(v2.RelativeOCIReferenceType, v2.Version),
		runtime.NewUnversionedType(v2.RelativeOCIReferenceType),
	)
}

// relativeScheme is a narrow scheme registering only the relative OCI reference type. It
// backs IsRelativeOCIReference: probing a Raw/Unstructured access against the broad Scheme
// would false-match any registered type (the Raw->Typed path does a bare json.Unmarshal
// with no type-match check), so classification must use a scheme that registers only the
// relative type.
var relativeScheme = runtime.NewScheme()

func init() {
	relativeScheme.MustRegisterWithAlias(&v2.RelativeOCIReference{},
		runtime.NewVersionedType(v2.RelativeOCIReferenceType, v2.Version),
		runtime.NewUnversionedType(v2.RelativeOCIReferenceType),
	)
}

// IsRelativeOCIReference reports whether access is a relativeOciReference (either spelling,
// as a concrete type, Raw, or Unstructured).
func IsRelativeOCIReference(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	var rel v2.RelativeOCIReference
	return relativeScheme.Convert(access, &rel) == nil
}
