package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var scheme = runtime.NewScheme()

func init() {
	MustRegisterIdentityType(scheme)
}

func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitIdentity{}, VersionedType, Type)
}
