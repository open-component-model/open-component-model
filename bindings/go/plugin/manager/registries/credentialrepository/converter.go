package credentialrepository

import (
	"context"
	"encoding/json"
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

// ResolveTyped converts the internal typed interface call to the external contract format.
func (c *credentialRepositoryPluginConverter) ResolveTyped(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials runtime.Typed) (runtime.Typed, error) {
	request := v1.ResolveRequest[runtime.Typed]{
		Config:   cfg,
		Identity: identity,
	}
	result, err := c.externalPlugin.ResolveTyped(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve typed credentials: %w", err)
	}
	return result, nil
}

// Resolve is a deprecated shim that calls ResolveTyped with map↔typed conversions.
func (c *credentialRepositoryPluginConverter) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	var typedCreds runtime.Typed
	if len(credentials) > 0 {
		typedCreds = &runtime.Raw{}
		data, err := json.Marshal(credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal credentials: %w", err)
		}
		if err := json.Unmarshal(data, typedCreds); err != nil {
			return nil, fmt.Errorf("failed to wrap credentials as Raw: %w", err)
		}
	}
	result, err := c.ResolveTyped(ctx, cfg, identity, typedCreds)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal typed result: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to convert typed result to map: %w", err)
	}
	return m, nil
}
