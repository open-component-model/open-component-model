package credentials

import (
	"ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers RSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.RSACredentials{},
		v1.VersionedType,
		runtime.NewUnversionedType(v1.RSACredentialsType),
	)
}
