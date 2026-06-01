package credentialtype

import (
	"context"

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
	return r.scheme
}

func (r *Registry) Register(scheme *runtime.Scheme) {
	r.scheme.MustRegisterScheme(scheme)
}

func (r *Registry) RegisterFromPlugin(credentialTypes []types.Type) {
	for _, t := range credentialTypes {
		_ = r.scheme.RegisterWithAlias(&runtime.Raw{}, t.Type)
	}
}
