package constructorrepositroy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/construction/v1"
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

	// inputJSONSchema is the schema for all endpoints for this plugin.
	inputJSONSchema []byte

	// accessJSONSchema is the schema for access endpoints for this plugin.
	accessJSONSchema []byte

	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ v1.ConstructionContract = (*RepositoryPlugin)(nil)
)

func NewConstructionRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, jsonSchema []byte) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:              id,
		path:            path,
		config:          config,
		client:          client,
		inputJSONSchema: jsonSchema,
		location:        loc,
	}
}

func (r *RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", r.ID)

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, request v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	response := v1.GetIdentityResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource input %s: %w", r.ID, err)
	}

	return response.Identity, nil
}

func (r *RepositoryPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceResponse, error) {
	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Resource.Input, r.inputJSONSchema); err != nil {
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

func (r *RepositoryPlugin) ProcessResourceDigest(ctx context.Context, resource descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	if err := r.validateEndpoint(resource.Access, r.accessJSONSchema); err != nil {
		return nil, err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	body := &descriptor.Resource{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ProcessResourceDigest, http.MethodPost, plugins.WithPayload(resource), plugins.WithResult(body), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource digest %s: %w", r.ID, err)
	}

	return body, nil
}

func (r *RepositoryPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceResponse, error) {
	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Source.Input, r.inputJSONSchema); err != nil {
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
