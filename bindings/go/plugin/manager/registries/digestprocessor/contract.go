package digestprocessor

import (
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BuiltinDigestProcessorPlugin interface {
	constructor.ResourceDigestProcessor
	GetResourceRepositoryScheme() *runtime.Scheme
}
