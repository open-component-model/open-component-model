package credentials

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// toIdentity converts a runtime.Typed to a runtime.Identity via type assertion.
//
// Deprecated: This is migration scaffolding used only for the AnyConsumerIdentityType
// fallback lookup in resolveFromRepository. Once the fallback logic migrates to work
// with runtime.Typed directly (Phase 4+), this function can be removed.
// https://github.com/open-component-model/ocm-project/issues/1047
func toIdentity(typed runtime.Typed) (runtime.Identity, error) {
	if typed == nil {
		return nil, fmt.Errorf("cannot convert nil to identity")
	}
	id, ok := typed.(runtime.Identity)
	if !ok {
		return nil, fmt.Errorf("cannot convert %T to runtime.Identity", typed)
	}
	return id, nil
}
