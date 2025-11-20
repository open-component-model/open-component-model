package credentialrepository

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BuiltinCredentialRepositoryPlugin interface {
	credentials.RepositoryPlugin
	GetCredentialRepositoryScheme() *runtime.Scheme
}
