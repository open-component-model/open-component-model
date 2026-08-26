package credentialplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	credentialpluginv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Endpoints
const (
	// GetConsumerIdentityEndpoint defines the endpoint to get consumer identity for a credential.
	GetConsumerIdentityEndpoint = "/consumer-identity"
	// ResolveEndpoint defines the endpoint to resolve credentials using the credential graph.
	ResolveEndpoint = "/resolve"
)

type CredentialPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	capability credentialpluginv1.CapabilitySpec
	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ credentialpluginv1.CredentialPluginContract[runtime.Typed] = &CredentialPlugin{}
)

// NewCredentialPlugin creates a new credential plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and capability spec.
func NewCredentialPlugin(client *http.Client, id string, path string, config types.Config, loc string, capability credentialpluginv1.CapabilitySpec) *CredentialPlugin {
	return &CredentialPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   loc,
	}
}

func (p *CredentialPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", p.ID)

	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", p.ID, err)
	}

	return nil
}

func (p *CredentialPlugin) GetConsumerIdentity(ctx context.Context, request credentialpluginv1.GetConsumerIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	slog.InfoContext(ctx, "Getting consumer identity", "id", p.ID)

	if err := p.validateEndpoint(request.Credential); err != nil {
		return nil, err
	}

	var identity runtime.Identity
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, GetConsumerIdentityEndpoint, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get consumer identity from plugin %q: %w", p.ID, err)
	}

	return identity, nil
}

func (p *CredentialPlugin) Resolve(ctx context.Context, request credentialpluginv1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error) {
	slog.InfoContext(ctx, "Resolving credentials", "id", p.ID)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	resolvedCredentials := &runtime.Raw{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, ResolveEndpoint, http.MethodPost, plugins.WithPayload(request), plugins.WithHeader(credHeader), plugins.WithResult(resolvedCredentials)); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials from plugin %q: %w", p.ID, err)
	}

	return resolvedCredentials, nil
}

// validateEndpoint uses the provided JSON schema and the runtime.Typed and, using the JSON schema, validates that the
// underlying runtime.Type conforms to the provided schema.
// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (p *CredentialPlugin) validateEndpoint(obj runtime.Typed) error {
	var schema []byte
	for _, t := range p.capability.SupportedCredentialPluginTypes {
		if t.Type != obj.GetType() {
			continue
		}
		schema = t.JSONSchema
	}

	valid, err := plugins.ValidatePlugin(obj, schema)
	if err != nil {
		return fmt.Errorf("failed to validate plugin %q: %w", p.ID, err)
	}
	if !valid {
		return fmt.Errorf("validation of plugin %q failed", p.ID)
	}

	return nil
}

func toCredentials(credentials runtime.Typed) (plugins.KV, error) {
	if credentials == nil {
		return plugins.KV{}, nil
	}
	rawCreds, err := json.Marshal(credentials)
	if err != nil {
		return plugins.KV{}, err
	}
	return plugins.KV{
		Key:   "Authorization",
		Value: string(rawCreds),
	}, nil
}
