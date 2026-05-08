package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers RSA/v1 (with v1alpha1 and unversioned aliases) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&RSAIdentity{},
		VersionedType,
		V1Alpha1Type, // backward-compat alias
		Type,         // unversioned alias
	)
}
