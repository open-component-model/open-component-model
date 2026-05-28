package credentialtype

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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

func NewRegistry() *Registry {
	return &Registry{
		scheme: runtime.NewScheme(),
	}
}

// GetCredentialTypeScheme - The credential graph calls this to obtain the scheme used during ingestion.
func (r *Registry) GetCredentialTypeScheme() *runtime.Scheme {
	return r.scheme
}

// Register calls fn with the registry's scheme, allowing a built-in binding to register
// its credential types.
func (r *Registry) Register(fn func(*runtime.Scheme)) {
	fn(r.scheme)
}

// RegisterFromPlugin registers credential types declared in an external plugin's CapabilitySpec.
// External plugin types are registered as *runtime.Raw.
// The credential graph will resolve them as *runtime.Raw instead of
// falling back to *DirectCredentials — consumers use scheme.Convert to get typed structs.
func (r *Registry) RegisterFromPlugin(capSpec *credentialrepositoryv1.CapabilitySpec) {
	for _, t := range capSpec.SupportedCredentialTypes {
		// Register the type with *runtime.Raw as the prototype. If already registered
		// (e.g. built-in type declared again by an external plugin), skip silently.
		_ = r.scheme.RegisterWithAlias(&runtime.Raw{}, t.Type)
	}
}
