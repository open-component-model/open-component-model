package credentials

import (
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TypeSchemeProvider provides read access to a runtime.Scheme of known types.
// The credential graph uses this during ingestion to deserialize typed credentials
// and resolve type aliases.
type TypeSchemeProvider interface {
	Scheme() *runtime.Scheme
}

// IdentityTypeRegistry stores consumer identity types and their accepted credential types.
// It wraps a runtime.Scheme for identity type deserialization and adds a mapping from
// identity types to their accepted credential types. This replaces the CredentialAcceptor
// interface — the mapping is declarative (stored at registration time) rather than
// behavioral (implemented on identity structs).
//
// The graph uses this registry during ingestion to validate that configured credential
// types are compatible with their identity types. See [validateConsumerIdentityTypes].
type IdentityTypeRegistry struct {
	mu            sync.RWMutex
	scheme        *runtime.Scheme
	acceptedCreds map[runtime.Type][]runtime.Type // identity type → accepted credential types
}

// NewIdentityTypeRegistry creates a new IdentityTypeRegistry with the given scheme options.
func NewIdentityTypeRegistry(opts ...runtime.SchemeOption) *IdentityTypeRegistry {
	return &IdentityTypeRegistry{
		scheme:        runtime.NewScheme(opts...),
		acceptedCreds: make(map[runtime.Type][]runtime.Type),
	}
}

// Scheme returns the underlying runtime.Scheme for identity type deserialization.
// This satisfies TypeSchemeProvider.
func (r *IdentityTypeRegistry) Scheme() *runtime.Scheme {
	return r.scheme
}

// Register registers an identity type prototype with its type names.
// Use RegisterWithAcceptedCredentials to also declare accepted credential types.
func (r *IdentityTypeRegistry) Register(prototype runtime.Typed, types ...runtime.Type) error {
	return r.scheme.RegisterWithAlias(prototype, types...)
}

// RegisterWithAcceptedCredentials registers an identity type prototype and declares which
// credential types it accepts. The first type in types is the default; the rest are aliases.
// The accepted credential types are stored for validation during ingestion.
func (r *IdentityTypeRegistry) RegisterWithAcceptedCredentials(
	prototype runtime.Typed,
	types []runtime.Type,
	acceptedCredentialTypes []runtime.Type,
) error {
	if err := r.scheme.RegisterWithAlias(prototype, types...); err != nil {
		return err
	}

	if len(acceptedCredentialTypes) > 0 && len(types) > 0 {
		r.mu.Lock()
		defer r.mu.Unlock()
		// Store under the default type (first in the list).
		r.acceptedCreds[types[0]] = acceptedCredentialTypes
	}

	return nil
}

// AcceptedCredentialTypes returns the credential types accepted by the given identity type.
// It resolves aliases through the scheme so that e.g. querying with an unversioned alias
// returns the same result as querying with the versioned default.
// Returns nil, false if the identity type has no declared accepted credential types.
func (r *IdentityTypeRegistry) AcceptedCredentialTypes(identityType runtime.Type) ([]runtime.Type, bool) {
	resolved := r.scheme.ResolveCanonicalType(identityType)

	r.mu.RLock()
	defer r.mu.RUnlock()

	accepted, ok := r.acceptedCreds[resolved]
	return accepted, ok
}
