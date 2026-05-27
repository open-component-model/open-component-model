package credentials

import (
	"ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(Scheme)
}

// MustRegisterCredentialType registers HelmHTTPCredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	v1.MustRegisterCredentialType(scheme)
}
