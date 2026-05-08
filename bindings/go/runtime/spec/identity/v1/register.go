package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers Identity/v1 (with the unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&Identity{},
		VersionedType,
		Type, // unversioned alias
	)
}
