package credentials

import (
	"ocm.software/open-component-model/bindings/go/notation/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers NotationCredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.NotationCredentials{},
		v1.VersionedType,
		runtime.NewUnversionedType(v1.NotationCredentialsType),
	)
}
