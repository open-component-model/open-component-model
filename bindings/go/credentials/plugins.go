// Package credentials provides a flexible and extensible credential management system
// for the Open Component Model (OCM).
package credentials

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// AnyCredentialType represents a wildcard credential type that can match any credential type.
// It is used when attempting to resolve credentials without a specific type constraint.
var AnyCredentialType = runtime.NewUnversionedType("*")

// RepositoryPlugin defines the interface for plugins that handle repository-specific
// credential resolution. These plugins are responsible for:
// - Identifying supported repository configuration types
// - Mapping repository configurations to consumer identities
// - Resolving credentials for specific repository configurations
type RepositoryPlugin interface {
	// SupportedRepositoryConfigTypes returns a list of repository configuration types
	// that this plugin can handle. The plugin will only be used for configurations
	// matching one of these types.
	SupportedRepositoryConfigTypes() []runtime.Type

	// ConsumerIdentityForConfig maps a repository configuration to a consumer identity.
	// This identity is used to look up credentials in the credential graph.
	ConsumerIdentityForConfig(ctx context.Context, config runtime.Typed) (runtime.Identity, error)

	// Resolve attempts to resolve credentials for a given repository configuration
	// and consumer identity. The provided credentials map may contain pre-resolved
	// credentials from the credential graph.
	Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
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
	Resolve(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
}

// GetRepositoryPluginFn is a function type that returns a RepositoryPlugin for a given
// typed object. This function is used to dynamically load repository plugins at runtime.
type GetRepositoryPluginFn func(ctx context.Context, typed runtime.Typed) (RepositoryPlugin, error)

// GetCredentialPluginFn is a function type that returns a CredentialPlugin for a given
// typed object. This function is used to dynamically load credential plugins at runtime.
type GetCredentialPluginFn func(ctx context.Context, typed runtime.Typed) (CredentialPlugin, error)
