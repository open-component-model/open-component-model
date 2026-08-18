package credentialtyperepository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CredentialTypeRegistry struct {
	ctx context.Context
	mu  sync.Mutex
	// registry records which plugin declared a credential type. It exists for conflict
	// attribution only; this registry never starts or owns a plugin process.
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
//
// Registration is idempotent: more than one plugin of a binding may declare the same types
// (the helm input method, resource repository and digest processor all consume
// HelmHTTPCredentials). Types that are already registered for the same prototype are skipped.
// A type already registered for a different prototype is a genuine conflict and returns an
// error.
func (r *CredentialTypeRegistry) Register(scheme *runtime.Scheme) error {
	if scheme == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for typ, aliases := range scheme.GetTypes() {
		prototype, err := scheme.NewObject(typ)
		if err != nil {
			return fmt.Errorf("failed to create prototype for credential type %q: %w", typ, err)
		}

		// Collect the types that are not known yet; already known ones must agree on the
		// prototype, otherwise two bindings claim the same type for different Go structs.
		missing := make([]runtime.Type, 0, len(aliases)+1)
		for _, candidate := range append([]runtime.Type{typ}, aliases...) {
			if !r.scheme.IsRegistered(candidate) {
				missing = append(missing, candidate)

				continue
			}

			registered, err := r.scheme.NewObject(candidate)
			if err != nil {
				return fmt.Errorf("failed to create prototype for registered credential type %q: %w", candidate, err)
			}
			if reflect.TypeOf(registered) != reflect.TypeOf(prototype) {
				return fmt.Errorf("credential type %q is already registered as %T and cannot be registered as %T", candidate, registered, prototype)
			}
		}

		if len(missing) == 0 {
			continue
		}

		if err := r.scheme.RegisterWithAlias(prototype, missing...); err != nil {
			return fmt.Errorf("failed to register credential type %q: %w", typ, err)
		}
	}

	return nil
}

// GetCredentialTypeScheme returns the runtime scheme containing all registered
// credential types, including built-in and plugin-declared custom types.
func (r *CredentialTypeRegistry) GetCredentialTypeScheme() *runtime.Scheme {
	return r.scheme
}

// RegisterCustomTypes registers credential types declared by an external plugin in one of its
// capability specs. The caller extracts them, so this registry stays independent of the
// capability contracts. External plugin types have no compiled-in Go type, so they are registered as
// [runtime.Raw] and consumers convert them via the scheme.
//
// The declaring plugin is remembered per type, so a plugin that declares the same type in more
// than one of its capability specs is a no-op, while a second plugin claiming a type that is
// already taken is a conflict. Conflicting entries are skipped and their errors are joined into
// the return value; non-conflicting entries are always registered, even when other entries
// conflict.
func (r *CredentialTypeRegistry) RegisterCustomTypes(plugin mtypes.Plugin, types []mtypes.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, t := range types {
		typed := &runtime.Raw{}
		typed.SetType(t.Type)
		allTypes := append([]runtime.Type{t.Type}, t.Aliases...)

		conflict := false
		// Collect the types that are not known yet. Registering an already known type again is
		// rejected by the scheme, which would drop the aliases behind it, so only the missing ones
		// are handed over.
		missing := make([]runtime.Type, 0, len(allTypes))
		for _, alias := range allTypes {
			if !r.scheme.IsRegistered(alias) {
				missing = append(missing, alias)

				continue
			}
			// Already known: only the plugin that declared it may declare it again, e.g. from a
			// second capability spec of the same plugin binary.
			if declaredBy, ok := r.registry[alias]; ok && declaredBy.ID == plugin.ID {
				continue
			}
			errs = append(errs, fmt.Errorf("credential type %s already registered", alias))
			conflict = true
		}
		if conflict {
			continue
		}
		if len(missing) == 0 {
			continue
		}

		if err := r.scheme.RegisterWithAlias(typed, missing...); err != nil {
			slog.ErrorContext(r.ctx, "failed to build scheme for plugin credential type", "type", t.Type, "error", err)
			errs = append(errs, err)

			continue
		}
		for _, alias := range allTypes {
			r.registry[alias] = plugin
		}
	}

	return errors.Join(errs...)
}

// RegisterInternalCredentialTypeSchemeProvider can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func (r *CredentialTypeRegistry) RegisterInternalCredentialTypeSchemeProvider(
	plugin BuiltinCredentialTypeSchemeProviderPlugin,
) error {
	if plugin == nil {
		return fmt.Errorf("cannot register nil credential type scheme provider")
	}

	// Register takes the lock itself, so it must not be held here.
	if err := r.Register(plugin.GetCredentialTypeScheme()); err != nil {
		return fmt.Errorf("failed to register credential types of %T: %w", plugin, err)
	}

	return nil
}
