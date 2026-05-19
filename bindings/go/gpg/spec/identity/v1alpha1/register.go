package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers GPG/v1alpha1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GPGIdentity{},
		V1Alpha1Type,
		Type, // unversioned alias
	)
}
