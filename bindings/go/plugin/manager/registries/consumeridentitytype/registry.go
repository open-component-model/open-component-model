package consumeridentitytype

import (
	"context"
	"log/slog"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.IdentityTypeSchemeProvider = &Registry{}

// Registry manages known consumer identity types (e.g. OCIRegistry/v1, HelmChartRepository/v1).
// Builtin bindings register Go structs directly; external plugins register
// as runtime.Raw from their capability specs. Builtins always take precedence —
// types already registered are skipped when plugins declare overlapping types.
type Registry struct {
	mu     sync.Mutex
	scheme *runtime.Scheme
}

// NewRegistry creates a new consumer identity type registry.
func NewRegistry() *Registry {
	return &Registry{
		scheme: runtime.NewScheme(),
	}
}

// Scheme returns the underlying runtime.Scheme for read access by the graph.
func (r *Registry) Scheme() *runtime.Scheme {
	return r.scheme
}

// MustRegister registers a typed identity prototype with the given types.
// Panics if registration fails.
func (r *Registry) MustRegister(prototype runtime.Typed, types ...runtime.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scheme.MustRegisterWithAlias(prototype, types...)
}

// RegisterFromPlugin registers a consumer identity type declared by an external plugin.
// Types already registered by builtins are skipped — builtins always take precedence.
func (r *Registry) RegisterFromPlugin(ctx context.Context, idType runtime.Type, aliases ...runtime.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scheme.IsRegistered(idType) {
		return
	}
	types := append([]runtime.Type{idType}, aliases...)
	if err := r.scheme.RegisterWithAlias(&runtime.Raw{}, types...); err != nil {
		slog.WarnContext(ctx, "could not register plugin consumer identity type",
			"type", idType.String(), "error", err)
	}
}
