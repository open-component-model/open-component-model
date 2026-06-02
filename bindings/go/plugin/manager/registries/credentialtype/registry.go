package credentialtype

import (
	"context"
	"log/slog"

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
		sub := runtime.NewScheme()
		allTypes := append([]runtime.Type{t.Type}, t.Aliases...)
		if err := sub.RegisterWithAlias(&runtime.Raw{}, allTypes...); err != nil {
			slog.Error("failed to build scheme for plugin credential type", "type", t.Type, "error", err)
			continue
		}
		if err := r.scheme.RegisterScheme(sub); err != nil {
			slog.Error("failed to register plugin credential type", "type", t.Type, "error", err)
		}
	}
}
