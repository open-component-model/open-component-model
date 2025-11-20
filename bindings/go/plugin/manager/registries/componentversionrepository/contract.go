package componentversionrepository

import (
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BuiltinComponentVersionRepositoryProvider interface {
	repository.ComponentVersionRepositoryProvider
	GetComponentVersionRepositoryScheme() *runtime.Scheme
}
