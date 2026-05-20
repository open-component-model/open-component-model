package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers RSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&RSACredentials{},
		runtime.NewVersionedType(RSACredentialsType, Version),
		runtime.NewUnversionedType(RSACredentialsType),
	)
}