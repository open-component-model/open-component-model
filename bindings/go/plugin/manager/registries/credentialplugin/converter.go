package credentialplugin

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// credentialPluginConverter converts between the external v1.CredentialPluginContract interface
// and the internal credentials.CredentialPlugin interface used internally.
type credentialPluginConverter struct {
	externalPlugin v1.CredentialPluginContract[runtime.Typed]
}

var _ credentials.CredentialPlugin = (*credentialPluginConverter)(nil)

// NewCredentialPluginConverter creates a new converter that wraps an external CredentialPluginContract
// to implement the internal credentials.CredentialPlugin interface.
func NewCredentialPluginConverter(plugin v1.CredentialPluginContract[runtime.Typed]) credentials.CredentialPlugin {
	return &credentialPluginConverter{
		externalPlugin: plugin,
	}
}

// GetConsumerIdentity converts the internal interface call to the external contract format.
func (c *credentialPluginConverter) GetConsumerIdentity(ctx context.Context, credential runtime.Typed) (runtime.Identity, error) {
	request := v1.GetConsumerIdentityRequest[runtime.Typed]{
		Credential: credential,
	}
	identity, err := c.externalPlugin.GetConsumerIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get consumer identity: %w", err)
	}
	return identity, nil
}

// Resolve converts the internal interface call to the external contract format.
func (c *credentialPluginConverter) Resolve(ctx context.Context, identity runtime.Identity, credentials runtime.Typed) (runtime.Typed, error) {
	request := v1.ResolveRequest[runtime.Typed]{
		Identity: identity,
	}
	resolvedCredentials, err := c.externalPlugin.Resolve(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}
	return resolvedCredentials, nil
}
