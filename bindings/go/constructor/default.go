package constructor

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	// Scheme is the default runtime scheme for the registry
	Scheme = runtime.NewScheme()
	// DefaultInputMethodRegistry is the default registry instance using the default scheme
	DefaultInputMethodRegistry = New(Scheme)
)
