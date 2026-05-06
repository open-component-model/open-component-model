package v1

import "ocm.software/open-component-model/bindings/go/runtime"

func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&SigstoreSignerIdentity{},
		VersionedType,
		V1Alpha1Type,
		Type,
	)
}
