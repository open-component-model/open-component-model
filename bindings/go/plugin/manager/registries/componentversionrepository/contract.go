package componentversionrepository

import (
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The BuiltinComponentVersionRepositoryProvider has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinComponentVersionRepositoryProvider interface {
	repository.ComponentVersionRepositoryProvider
	GetComponentVersionRepositoryScheme() *runtime.Scheme
}
