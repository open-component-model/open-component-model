// Package identity provides the scheme containing the TSA consumer identity types.
package identity

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/identity/v1alpha1"
)

// Scheme holds the registered TSA consumer identity types.
var Scheme = runtime.NewScheme()

func init() {
	v1alpha1.MustRegisterIdentityType(Scheme)
}
