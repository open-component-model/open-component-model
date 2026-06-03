package credentialtype

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.CredentialTypeSchemeProvider = (*Registry)(nil)

// Registry holds the credential type scheme used by the credential graph to deserialize
// typed consumer credentials from .ocmconfig.
//
// Built-in bindings register their credential types via Register during startup.
// External plugins register their custom credential types from capability specs during plugin
// discovery via RegisterFromPlugin.
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

func (r *Registry) RegisterFromPlugin(credentialTypes []types.Type) error {
	r.Lock()
	defer r.Unlock()
	var errs []error
	for _, t := range credentialTypes {
		typed := &runtime.Raw{}
		typed.SetType(t.Type)
		allTypes := append([]runtime.Type{t.Type}, t.Aliases...)
		conflict := false
		for _, alias := range allTypes {
			if r.scheme.IsRegistered(alias) {
				errs = append(errs, fmt.Errorf("credential type %s already registered", alias))
				conflict = true
			}
		}
		if conflict {
			continue
		}
		if err := r.scheme.RegisterWithAlias(typed, allTypes...); err != nil {
			slog.ErrorContext(r.ctx, "failed to build scheme for plugin credential type", "type", t.Type, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
