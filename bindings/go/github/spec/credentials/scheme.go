// Package credentials exposes the credential types the GitHub access method
// understands, pre-registered in a scheme.
package credentials

import (
	v1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterCredentialType(Scheme)
}
