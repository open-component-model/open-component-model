package credentials

import (
	"fmt"
	"log/slog"
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// IdentityTypeRegistry stores consumer identity types and their accepted credential types.
// Each registered identity type may have one or more aliases; the first type passed at
// registration is the canonical type, the rest are aliases that resolve to it.
//
// The graph uses this registry during ingestion to validate that configured credential
// types are compatible with their identity types. See [validateConsumerIdentityTypes].
type IdentityTypeRegistry struct {
	mu            sync.RWMutex
	canonicalOf   map[runtime.Type]runtime.Type   // any registered type → canonical (default) type
	acceptedCreds map[runtime.Type][]runtime.Type // canonical type → accepted credential types
}

// NewIdentityTypeRegistry creates a new, empty IdentityTypeRegistry.
func NewIdentityTypeRegistry() *IdentityTypeRegistry {
	return &IdentityTypeRegistry{
		canonicalOf:   make(map[runtime.Type]runtime.Type),
		acceptedCreds: make(map[runtime.Type][]runtime.Type),
	}
}

// Register registers an identity type and its aliases. The first type is the canonical type;
// the rest are aliases that resolve to it. Use RegisterWithAcceptedCredentials to also declare
// accepted credential types.
//
// Returns a [runtime.TypeAlreadyRegisteredError] if any of the types is already registered
// against a different canonical.
func (r *IdentityTypeRegistry) Register(types ...runtime.Type) error {
	if len(types) == 0 {
		return fmt.Errorf("at least one identity type is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	canonical := types[0]
	for _, t := range types {
		if existing, ok := r.canonicalOf[t]; ok && !existing.Equal(canonical) {
			return fmt.Errorf("%w: as alias for %q", runtime.TypeAlreadyRegisteredError(t), existing)
		}
	}
	for _, t := range types {
		r.canonicalOf[t] = canonical
	}
	return nil
}

// RegisterWithAcceptedCredentials registers an identity type (with optional aliases) and declares
// which credential types it accepts. The first type in types is the canonical type.
// The accepted credential types are stored under the canonical type for validation during ingestion.
func (r *IdentityTypeRegistry) RegisterWithAcceptedCredentials(
	types []runtime.Type,
	acceptedCredentialTypes []runtime.Type,
) error {
	if err := r.Register(types...); err != nil {
		return err
	}

	if len(acceptedCredentialTypes) > 0 {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.acceptedCreds[types[0]] = acceptedCredentialTypes
	}

	return nil
}

// IsRegistered reports whether the given identity type (or one of its aliases) has been registered.
func (r *IdentityTypeRegistry) IsRegistered(identityType runtime.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.canonicalOf[identityType]
	return ok
}

// AcceptedCredentialTypes returns the credential types accepted by the given identity type.
// It resolves aliases internally so that querying with an unversioned alias returns the same
// result as querying with the versioned canonical type.
// Returns nil, false if the identity type is unknown or has no declared accepted credential types.
func (r *IdentityTypeRegistry) AcceptedCredentialTypes(identityType runtime.Type) ([]runtime.Type, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	canonical, ok := r.canonicalOf[identityType]
	if !ok {
		slog.Debug("IdentityTypeRegistry: identity type not registered", "identityType", identityType)
		return nil, false
	}

	accepted, ok := r.acceptedCreds[canonical]
	return accepted, ok
}
