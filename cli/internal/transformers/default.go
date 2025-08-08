package transformers

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var DefaultTransformers = map[string]runtime.Typed{}

func Transformers() map[string]runtime.Typed {
	return DefaultTransformers
}
