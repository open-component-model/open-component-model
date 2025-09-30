package manager

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// EnsureTyped ensures that a runtime.Typed is not a Raw object.
// If it is a Raw object, it will be converted using the provided scheme.
func EnsureTyped(t runtime.Typed, scheme *runtime.Scheme) (runtime.Typed, error) {
	typ := t.GetType()
	if raw, ok := t.(*runtime.Raw); ok {
		var converted runtime.Typed
		if err := scheme.Convert(raw, converted); err != nil {
			return nil, fmt.Errorf("failed to convert raw to type %v: %w", typ, err)
		}
		t = converted
	}
	return t, nil
}
