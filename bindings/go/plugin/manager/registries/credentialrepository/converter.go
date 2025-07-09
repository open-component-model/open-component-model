package credentialrepository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// credentialRepositoryPluginConverter converts between the external v1.CredentialRepositoryPluginContract interface
// and the internal credentials.RepositoryPlugin interface used internally.
// It implements the internal interface by wrapping external plugin calls.
type credentialRepositoryPluginConverter struct {
	externalPlugin v1.CredentialRepositoryPluginContract[runtime.Typed]
}

var _ credentials.RepositoryPlugin = (*credentialRepositoryPluginConverter)(nil)

// NewCredentialRepositoryPluginConverter creates a new converter that wraps an external CredentialRepositoryPluginContract
// to implement the internal credentials.RepositoryPlugin interface.
func NewCredentialRepositoryPluginConverter(plugin v1.CredentialRepositoryPluginContract[runtime.Typed]) credentials.RepositoryPlugin {
	return &credentialRepositoryPluginConverter{
		externalPlugin: plugin,
	}
}

// ConsumerIdentityForConfig converts the internal interface call to the external contract format.
// It wraps the config in a ConsumerIdentityForConfigRequest and calls the external plugin.
func (c *credentialRepositoryPluginConverter) ConsumerIdentityForConfig(ctx context.Context, config runtime.Typed) (runtime.Identity, error) {
	request := v1.ConsumerIdentityForConfigRequest[runtime.Typed]{
		Config: config,
	}
	identity, err := c.externalPlugin.ConsumerIdentityForConfig(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get consumer identity for config: %w", err)
	}
	return identity, nil
}

// Resolve converts the internal interface call to the external contract format.
// It wraps the config and identity in a ResolveRequest and calls the external plugin.
func (c *credentialRepositoryPluginConverter) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	request := v1.ResolveRequest[runtime.Typed]{
		Config:   cfg,
		Identity: identity,
	}

	resolvedCredentials, err := c.externalPlugin.Resolve(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}
	return resolvedCredentials, nil
}
