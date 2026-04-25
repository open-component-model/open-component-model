package credentials

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// toIdentity converts a runtime.Typed to a runtime.Identity.
// If the typed object is already an Identity, it is returned directly.
// If it implements runtime.IdentityProvider, ToIdentity() is called.
// Otherwise, an error is returned.
//
// Deprecated: This is migration scaffolding for Phase 1. It exists because plugin interfaces
// (CredentialPlugin, RepositoryPlugin) still accept runtime.Identity. Once those interfaces
// migrate to runtime.Typed in Phase 3 (see ADR 0018), this function and all call sites
// will be removed. https://github.com/open-component-model/ocm-project/issues/980
func toIdentity(typed runtime.Typed) (runtime.Identity, error) {
	if typed == nil {
		return nil, fmt.Errorf("cannot convert nil to identity")
	}
	if id, ok := typed.(runtime.Identity); ok {
		return id, nil
	}
	if p, ok := typed.(runtime.IdentityProvider); ok {
		return p.ToIdentity(), nil
	}
	return nil, fmt.Errorf("cannot convert %T to identity: does not implement IdentityProvider", typed)
}
