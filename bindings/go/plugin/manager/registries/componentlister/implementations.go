package componentlister

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	componentlisterv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ListComponents lists components in a repository.
	ListComponents = "/components/list"
	// Identity provides the identity of a type supported by the plugin.
	Identity = "/identity"
)

type ComponentListerPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	// jsonSchema is the schema for all endpoints for this plugin.
	capability componentlisterv1.CapabilitySpec

	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ componentlisterv1.ComponentListerPluginContract[runtime.Typed] = &ComponentListerPlugin{}
)

// NewComponentListerPlugin creates a new component lister plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and JSON schema.
func NewComponentListerPlugin(client *http.Client, id string, path string, config types.Config, loc string, capability componentlisterv1.CapabilitySpec) *ComponentListerPlugin {
	return &ComponentListerPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   loc,
	}
}

func (r *ComponentListerPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", r.ID)

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *ComponentListerPlugin) GetIdentity(ctx context.Context, request *componentlisterv1.GetIdentityRequest[runtime.Typed]) (*componentlisterv1.GetIdentityResponse, error) {
	if err := r.validateEndpoint(request.Typ); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", r.ID, err)
	}

	identity := componentlisterv1.GetIdentityResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", r.ID, err)
	}

	return &identity, nil
}

func (r *ComponentListerPlugin) ListComponents(ctx context.Context, request *componentlisterv1.ListComponentsRequest[runtime.Typed], credentials map[string]string) (*componentlisterv1.ListComponentsResponse, error) {
	response := &componentlisterv1.ListComponentsResponse{}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return response, err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return response, err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ListComponents, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component names from plug-in '%s' for request '%+v': %w", r.ID, *request, err)
	}

	return response, nil
}

// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (r *ComponentListerPlugin) validateEndpoint(obj runtime.Typed) error {
	var schema []byte
	for _, t := range r.capability.SupportedRepositorySpecTypes {
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
		return fmt.Errorf("validation of plugin %q failed for get local resource", r.ID)
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
