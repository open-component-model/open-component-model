package credentials

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// toIdentity converts a runtime.Typed to a runtime.Identity via type assertion.
//
// Deprecated: This is migration scaffolding for Phase 1. It exists because plugin interfaces
// (CredentialPlugin, RepositoryPlugin) and the graph's internal matching still work with
// runtime.Identity. Once those interfaces migrate to runtime.Typed in Phase 3 (see ADR 0018),
// this function and all call sites will be removed.
// https://github.com/open-component-model/ocm-project/issues/980
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
