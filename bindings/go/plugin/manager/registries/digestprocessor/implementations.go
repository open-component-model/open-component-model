package digestprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	digestprocessorv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type RepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config mtypes.Config
	path   string
	client *http.Client

	// jsonSchema is the schema for all endpoints for this plugin.
	capability digestprocessorv1.CapabilitySpec

	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ digestprocessorv1.ResourceDigestProcessorContract = (*RepositoryPlugin)(nil)
)

// NewDigestProcessorPlugin creates a new digest processor plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and JSON schema.
func NewDigestProcessorPlugin(client *http.Client, id string, path string, config mtypes.Config, loc string, capability digestprocessorv1.CapabilitySpec) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   loc,
	}
}

func (p *RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", p.ID)

	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", p.ID, err)
	}

	return nil
}

func (p *RepositoryPlugin) GetIdentity(ctx context.Context, request *digestprocessorv1.GetIdentityRequest[runtime.Typed]) (*digestprocessorv1.GetIdentityResponse, error) {
	if err := p.validateEndpoint(request.Typ); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	identity := digestprocessorv1.GetIdentityResponse{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", p.ID, err)
	}

	return &identity, nil
}

func (p *RepositoryPlugin) ProcessResourceDigest(ctx context.Context, request *digestprocessorv1.ProcessResourceDigestRequest, credentials map[string]string) (*digestprocessorv1.ProcessResourceDigestResponse, error) {
	// Note: We don't validate the resource here since it doesn't implement runtime.Typed
	// The validation should be handled by the plugin itself

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	result := &digestprocessorv1.ProcessResourceDigestResponse{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, ProcessResourceDigest, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&result), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource digest %s: %w", p.ID, err)
	}

	return result, nil
}

// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (p *RepositoryPlugin) validateEndpoint(obj runtime.Typed) error {
	var schema []byte
	for _, t := range p.capability.SupportedAccessTypes {
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
