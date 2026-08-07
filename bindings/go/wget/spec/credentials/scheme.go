package credentials

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

// Scheme holds the registered wget credential specification types.
var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterCredentialType(Scheme)
}
