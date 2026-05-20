package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	rsaCreds := &RSACredentials{}
	scheme.MustRegisterWithAlias(rsaCreds,
		runtime.NewVersionedType(RSACredentialsType, Version),
		runtime.NewUnversionedType(RSACredentialsType),
	)
}
