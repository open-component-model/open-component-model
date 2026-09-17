// Package credentials registers the ECDSA attestation credential type so it is
// preserved as a typed credential when resolved through the OCM credential graph.
package credentials

import (
	v1 "ocm.software/open-component-model/bindings/go/attestation/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme holds the registered ECDSA attestation credential types.
var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers ECDSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.ECDSACredentials{},
		v1.VersionedType,
		runtime.NewUnversionedType(v1.ECDSACredentialsType),
	)
}
