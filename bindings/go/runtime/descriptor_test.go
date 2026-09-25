package runtime_test

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_DescConvert() {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc := v2.Descriptor{}
	unstructured := runtime.Unstructured{}

	err := scheme.Convert(&desc, )
}
