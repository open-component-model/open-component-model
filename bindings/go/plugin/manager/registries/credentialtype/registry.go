package credentialtype

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// compile-time assertion: Registry satisfies the credential type scheme provider interface.
var _ credentials.CredentialTypeSchemeProvider = (*Registry)(nil)

// Registry holds the credential type scheme used by the credential graph to deserialize
// typed consumer credentials from .ocmconfig at ingestion time (ADR 0021 §Type Registries).
//
// Built-in bindings register their credential types via Register during startup.
// External plugins register their credential types from capability specs during plugin
// discovery via RegisterFromPlugin (not yet implemented).
type Registry struct {
	scheme *runtime.Scheme
}

// NewRegistry creates a new credential type registry with an empty scheme.
func NewRegistry() *Registry {
	return &Registry{
		scheme: runtime.NewScheme(),
	}
}

// GetCredentialTypeScheme implements credentials.CredentialTypeSchemeProvider.
// The credential graph calls this to obtain the scheme used during ingestion.
func (r *Registry) GetCredentialTypeScheme() *runtime.Scheme {
	return r.scheme
}

// Register calls fn with the registry's scheme, allowing a built-in binding to register
// its credential types. Follows the same self-registration pattern used by all other
// plugin type registries.
func (r *Registry) Register(fn func(*runtime.Scheme)) {
	fn(r.scheme)
}
