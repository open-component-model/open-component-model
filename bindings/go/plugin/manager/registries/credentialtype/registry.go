package credentialtype

import (
	"context"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.CredentialTypeSchemeProvider = (*Registry)(nil)

// Registry holds the credential type scheme used by the credential graph to deserialize
// typed consumer credentials from .ocmconfig.
//
// Built-in bindings register their credential types via Register during startup.
// Plugin credential types are registered through the credential repository registry.
type Registry struct {
	sync.RWMutex
	ctx    context.Context
	scheme *runtime.Scheme
}

func NewRegistry(ctx context.Context) *Registry {
	return &Registry{
		ctx:    ctx,
		scheme: runtime.NewScheme(),
	}
}

func (r *Registry) GetCredentialTypeScheme() *runtime.Scheme {
	r.RLock()
	defer r.RUnlock()
	return r.scheme
}

func (r *Registry) Register(scheme *runtime.Scheme) {
	r.Lock()
	defer r.Unlock()
	r.scheme.MustRegisterScheme(scheme)
}
