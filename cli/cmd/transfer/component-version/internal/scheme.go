package internal

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme(runtime.WithAllowUnknown())

func init() {
	Scheme.MustRegisterScheme(oci.Scheme)
	Scheme.MustRegisterScheme(descriptorv2.Scheme)
}
