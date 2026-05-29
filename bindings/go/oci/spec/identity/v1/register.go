package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var scheme = runtime.NewScheme()

func init() {
	MustRegisterIdentityType(scheme)
}

// LegacyType is the pre-#1964 identity type name kept as a scheme alias so
// that code paths that decode OCIRegistryIdentity from raw bytes (e.g. scheme.Convert)
// still work when the raw data carries the old type string.
var LegacyType = runtime.NewUnversionedType("OCIRepository")

// MustRegisterIdentityType registers OCIRegistry/v1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&OCIRegistryIdentity{},
		VersionedType,
		Type,       // backward-compat alias
		LegacyType, // pre-#1964 legacy alias
	)
}
