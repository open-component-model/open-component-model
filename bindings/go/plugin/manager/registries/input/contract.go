package input

import (
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The BuiltinResourceInputMethod has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinResourceInputMethod interface {
	constructor.ResourceInputMethod
	GetInputMethodScheme() *runtime.Scheme
}

// The BuiltinSourceInputMethod has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinSourceInputMethod interface {
	constructor.SourceInputMethod
	GetInputMethodScheme() *runtime.Scheme
}
