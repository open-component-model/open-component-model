package identity

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
)

var Scheme = runtime.NewScheme()

func init() {
	v1.MustRegisterIdentityType(Scheme)
}
