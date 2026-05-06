package credentialplugin

import (
	"context"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.CredentialPluginProvider = (*Registry)(nil)

// Registry holds registered credential plugins and resolves them by type.
type Registry struct {
	ctx             context.Context
	mu              sync.Mutex
	scheme          *runtime.Scheme
	internalPlugins map[runtime.Type]credentials.CredentialPlugin
}

// NewRegistry creates a new credential plugin registry.
func NewRegistry(ctx context.Context) *Registry {
	return &Registry{
		ctx:             ctx,
		scheme:          runtime.NewScheme(),
		internalPlugins: make(map[runtime.Type]credentials.CredentialPlugin),
	}
}

// RegisterInternalCredentialPlugin registers a builtin credential plugin for
// all types declared in its scheme.
func (r *Registry) RegisterInternalCredentialPlugin(plugin BuiltinCredentialPlugin) error {
	if plugin == nil {
		return fmt.Errorf("cannot register nil credential plugin")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetCredentialPluginScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetCredentialPluginScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internalPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalPlugins[alias] = plugin
		}
	}

	return nil
}

// GetCredentialPlugin returns the credential plugin for the given typed spec.
func (r *Registry) GetCredentialPlugin(_ context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
	if typed == nil {
		return nil, fmt.Errorf("credential plugin lookup requires a non-nil typed argument")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	_, _ = r.scheme.DefaultType(typed)
	typ := typed.GetType()
	if typ.IsEmpty() {
		return nil, fmt.Errorf("credential plugin lookup requires a type")
	}

	if plugin, ok := r.internalPlugins[typ]; ok {
		return plugin, nil
	}

	return nil, fmt.Errorf("no credential plugin registered for type %s", typ)
}

// Scheme returns the registry's type scheme.
func (r *Registry) Scheme() *runtime.Scheme {
	return r.scheme
}
