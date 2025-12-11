package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

var Scheme = runtime.NewScheme()

const Version = "v1alpha1"

var (
	MockGetObjectV1alpha1 = runtime.NewVersionedType(MockGetObjectTransformerType, Version)
	MockAddObjectV1alpha1 = runtime.NewVersionedType(MockAddObjectTransformerType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&MockGetObjectTransformer{}, MockGetObjectV1alpha1)
	Scheme.MustRegisterWithAlias(&MockAddObjectTransformer{}, MockAddObjectV1alpha1)
}
