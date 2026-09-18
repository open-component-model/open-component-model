package credentials

import (
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme holds the registered npm credential specification types.
var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterCredentialType(Scheme)
}
