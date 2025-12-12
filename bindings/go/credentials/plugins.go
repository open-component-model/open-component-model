package credentials

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// AnyConsumerIdentityType represents a wildcard consumer identity type that can match any consumer identity.
// It is used when attempting to resolve consumer identities without a specific type constraint.
var AnyConsumerIdentityType = runtime.NewUnversionedType("*")

// RepositoryPlugin defines the interface for plugins that handle repository-specific
// credential resolution. These plugins are responsible for:
// - Identifying supported repository configuration types
// - Mapping repository configurations to consumer identities
// - Resolving credentials for specific repository configurations
type RepositoryPlugin interface {
	// ConsumerIdentityForConfig maps a repository configuration to a consumer identity.
	// This identity is used to look up credentials in the credential graph.
	ConsumerIdentityForConfig(ctx context.Context, config runtime.Typed) (runtime.Identity, error)

	// Resolve attempts to resolve credentials for a given repository configuration
	// and consumer identity. The provided credentials map may contain pre-resolved
	// credentials from the credential graph.
	Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials runtime.Typed) (runtime.Typed, error)
}

// CredentialPlugin defines the interface for plugins that handle custom credential
// resolution logic. These plugins are responsible for:
// - Mapping credentials to consumer identities
// - Resolving credentials for specific identities
type CredentialPlugin interface {
	// GetConsumerIdentity maps a credential to a consumer identity.
	// This identity is used to look up credentials in the credential graph.
	GetConsumerIdentity(ctx context.Context, credential runtime.Typed) (runtime.Identity, error)

	// Resolve attempts to resolve credentials for a given consumer identity.
	// The provided credentials map may contain pre-resolved credentials from
	// the credential graph.
	Resolve(ctx context.Context, identity runtime.Identity, credentials runtime.Typed) (runtime.Typed, error)
}

// GetRepositoryPluginFn is a function type that returns a RepositoryPlugin for a given
// typed object. This function is used to dynamically load repository plugins at runtime.
type GetRepositoryPluginFn func(ctx context.Context, typed runtime.Typed) (RepositoryPlugin, error)

func (fn GetRepositoryPluginFn) GetRepositoryPlugin(ctx context.Context, typed runtime.Typed) (RepositoryPlugin, error) {
	return fn(ctx, typed)
}

// RepositoryPluginProvider is any provider that can retrieve a RepositoryPlugin for a given typed object.
type RepositoryPluginProvider interface {
	GetRepositoryPlugin(ctx context.Context, typed runtime.Typed) (RepositoryPlugin, error)
}

// GetCredentialPluginFn is a function type that returns a CredentialPlugin for a given
// typed object. This function is used to dynamically load credential plugins at runtime.
type GetCredentialPluginFn func(ctx context.Context, typed runtime.Typed) (CredentialPlugin, error)

func (fn GetCredentialPluginFn) GetCredentialPlugin(ctx context.Context, typed runtime.Typed) (CredentialPlugin, error) {
	return fn(ctx, typed)
}

// CredentialPluginProvider is any provider that can retrieve a CredentialPlugin for a given typed object.
type CredentialPluginProvider interface {
	GetCredentialPlugin(ctx context.Context, typed runtime.Typed) (CredentialPlugin, error)
}
