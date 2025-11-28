package credentialrepository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	credentialsv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Endpoints
const (
	// ConsumerIdentityForConfig defines the endpoint to get consumer identity for configuration.
	ConsumerIdentityForConfig = "/consumer-identity"
	// Resolve defines the endpoint to resolve credentials using the credential graph.
	Resolve = "/resolve"
)

type RepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	capability credentialsv1.CapabilitySpec
	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ credentialsv1.CredentialRepositoryPluginContract[runtime.Typed] = &RepositoryPlugin{}
)

// NewCredentialRepositoryPlugin creates a new credential repository plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and JSON schema.
func NewCredentialRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, capability credentialsv1.CapabilitySpec) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   loc,
	}
}

func (r *RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", r.ID)

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) ConsumerIdentityForConfig(ctx context.Context, cfg credentialsv1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
	slog.InfoContext(ctx, "Getting consumer identity for config", "id", r.ID)

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(cfg.Config); err != nil {
		return nil, err
	}

	var identity runtime.Identity
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ConsumerIdentityForConfig, http.MethodPost, plugins.WithPayload(cfg), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get consumer identity from plugin %q: %w", r.ID, err)
	}

	return identity, nil
}

func (r *RepositoryPlugin) Resolve(ctx context.Context, cfg credentialsv1.ResolveRequest[runtime.Typed], credentials map[string]string) (map[string]string, error) {
	slog.InfoContext(ctx, "Resolving credentials", "id", r.ID)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(cfg.Config); err != nil {
		return nil, err
	}

	var resolvedCredentials map[string]string
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Resolve, http.MethodPost, plugins.WithPayload(cfg), plugins.WithHeader(credHeader), plugins.WithResult(&resolvedCredentials)); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials from plugin %q: %w", r.ID, err)
	}

	return resolvedCredentials, nil
}

// validateEndpoint uses the provided JSON schema and the runtime.Typed and, using the JSON schema, validates that the
// underlying runtime.Type conforms to the provided schema.
// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (r *RepositoryPlugin) validateEndpoint(obj runtime.Typed) error {
	var schema []byte
	for _, t := range append(r.capability.SupportedConsumerIdentityTypes, r.capability.SupportedCredentialRepositorySpecTypes...) {
		if t.Type != obj.GetType() {
			continue
		}
		schema = t.JSONSchema
	}

	valid, err := plugins.ValidatePlugin(obj, schema)
	if err != nil {
		return fmt.Errorf("failed to validate plugin %q: %w", r.ID, err)
	}
	if !valid {
		return fmt.Errorf("validation of plugin %q failed", r.ID)
	}

	return nil
}

func toCredentials(credentials map[string]string) (plugins.KV, error) {
	rawCreds, err := json.Marshal(credentials)
	if err != nil {
		return plugins.KV{}, err
	}
	return plugins.KV{
		Key:   "Authorization",
		Value: string(rawCreds),
	}, nil
}
