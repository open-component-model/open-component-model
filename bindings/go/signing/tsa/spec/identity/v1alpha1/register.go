package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers TSA/v1alpha1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&TSAIdentity{},
		VersionedType,
		Type,
	)
}
