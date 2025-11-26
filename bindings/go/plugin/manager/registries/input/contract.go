package input

import (
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BuiltinResourceInputMethod interface {
	constructor.ResourceInputMethod
	GetInputMethodScheme() *runtime.Scheme
}

type BuiltinSourceInputMethod interface {
	constructor.SourceInputMethod
	GetInputMethodScheme() *runtime.Scheme
}
