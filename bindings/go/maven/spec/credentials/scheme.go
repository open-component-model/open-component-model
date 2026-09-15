// Package credentials exposes the credential types the Maven access type
// understands, pre-registered in a scheme.
package credentials

import (
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterCredentialType(Scheme)
}
