package credentialtyperepository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	credentialsv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CredentialTypeRegistry struct {
	ctx      context.Context
	mu       sync.Mutex
	registry map[runtime.Type]mtypes.Plugin

	scheme *runtime.Scheme
}

func NewCredentialTypeRegistry(ctx context.Context) *CredentialTypeRegistry {
	return &CredentialTypeRegistry{
		ctx:      ctx,
		registry: make(map[runtime.Type]mtypes.Plugin),
		scheme:   runtime.NewScheme(),
	}
}

// Register merges a pre-built scheme of built-in credential types into the registry.
// Call this during startup to make built-in types (e.g. OCICredentials, HelmHTTPCredentials)
// known before any plugin is loaded.
func (r *CredentialTypeRegistry) Register(scheme *runtime.Scheme) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scheme.MustRegisterScheme(scheme)
}

// GetCredentialTypeScheme returns the runtime scheme containing all registered
// credential types, including built-in and plugin-declared custom types.
func (r *CredentialTypeRegistry) GetCredentialTypeScheme() *runtime.Scheme {
	return r.scheme
}

func (r *CredentialTypeRegistry) registerCustomCredentialTypes(capability credentialsv1.CapabilitySpec) error {
	var errs []error
	for _, t := range capability.CustomCredentialTypes {
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

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *CredentialTypeRegistry) AddPlugin(plugin mtypes.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	//capability := credentialsv1.CapabilitySpec{}
	//if err := credentialsv1.Scheme.Convert(spec, &capability); err != nil {
	//	return fmt.Errorf("failed to convert object: %w", err)
	//}
	//if _, ok := r.capabilities[plugin.ID]; ok {
	//	return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	//}
	//r.capabilities[plugin.ID] = capability
	//
	//if err := r.registerCustomCredentialTypes(capability); err != nil {
	//	return fmt.Errorf("failed to register custom credential types: %w", err)
	//}

	return nil
}

// RegisterInternalCredentialTypeSchemeProvider can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func (r *CredentialTypeRegistry) RegisterInternalCredentialTypeSchemeProvider(
	plugin BuiltinCredentialTypeSchemeProviderPlugin,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	scheme := plugin.GetCredentialTypeScheme()

	r.Register(scheme)

	return nil
}
