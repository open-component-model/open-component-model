package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers OCIRegistry/v1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&OCIRegistryIdentity{},
		VersionedType,
		Type, // backward-compat alias
	)
}
