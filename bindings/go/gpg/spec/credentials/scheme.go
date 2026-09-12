package credentials

import (
	v1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	v1alpha1.MustRegisterCredentialType(Scheme)
}
