// Package credentials provides the scheme containing the TSA credential types.
package credentials

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
)

// Scheme holds the registered TSA credential types.
var Scheme = runtime.NewScheme()

func init() {
	v1alpha1.MustRegisterCredentialType(Scheme)
}
