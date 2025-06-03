package inputrepository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type RepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	// jsonSchema is the schema for all endpoints for this plugin.
	// for input repositories, this is the schema for the input type.
	jsonSchema []byte

	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ v1.InputPluginContract = (*RepositoryPlugin)(nil)
)

func NewConstructionRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, jsonSchema []byte) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		jsonSchema: jsonSchema,
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

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, request *v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	if err := r.validateEndpoint(request.Typ, r.jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", r.ID, err)
	}

	identity := runtime.Identity{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", r.ID, err)
	}

	return identity, nil
}

func (r *RepositoryPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceResponse, error) {
	if err := r.validateEndpoint(request.Resource.Input, r.jsonSchema); err != nil {
		return nil, err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	body := v1.ProcessResourceResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ProcessResource, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&body), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource input %s: %w", r.ID, err)
	}

	return &body, nil
}

func (r *RepositoryPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceResponse, error) {
	if err := r.validateEndpoint(request.Source.Input, r.jsonSchema); err != nil {
		return nil, err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	body := v1.ProcessSourceResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ProcessSource, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&body), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource input %s: %w", r.ID, err)
	}

	return &body, nil
}

func (r *RepositoryPlugin) validateEndpoint(obj runtime.Typed, jsonSchema []byte) error {
	valid, err := plugins.ValidatePlugin(obj, jsonSchema)
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
