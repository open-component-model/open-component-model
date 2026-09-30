// Package identity provides the scheme containing the wget consumer identity types,
// including the HTTP aliases under which the Wget identity can be declared.
package identity

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// Scheme holds the registered wget consumer identity types.
var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterIdentityType(Scheme)
}
