package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers Notation/v1 (with the unversioned alias)
// in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&NotationIdentity{},
		VersionedType,
		Type, // unversioned alias
	)
}
