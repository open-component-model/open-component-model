package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&SigstoreVerifierIdentity{},
		VersionedType,
		Type,
	)
}
