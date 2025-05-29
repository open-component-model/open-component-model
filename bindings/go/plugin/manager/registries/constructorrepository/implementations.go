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

	// TODO: Since we aren't typed, I'm not sure this is needed? Maybe for the input that we are about to resolve?
	// jsonSchema is the schema for all endpoints for this plugin.
	jsonSchema []byte
	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ v1.ConstructionContract = (*RepositoryPlugin)(nil)
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

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, typ v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	return runtime.Identity{}, nil
}

func (r *RepositoryPlugin) ProcessResource(ctx context.Context, request v1.ProcessResourceRequest, credentials map[string]string) (v1.ProcessResourceResponse, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return v1.ProcessResourceResponse{}, fmt.Errorf("error converting credentials: %w", err)
	}

	body := &v1.ProcessResourceResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ProcessResource, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(body), plugins.WithHeader(credHeader)); err != nil {
		return v1.ProcessResourceResponse{}, fmt.Errorf("failed to process resource input %s: %w", r.ID, err)
	}

	return *body, nil
}

func (r *RepositoryPlugin) ProcessResourceDigest(ctx context.Context, resource descriptor.Resource) (*descriptor.Resource, error) {
	// TODO implement me
	panic("implement me")
}

func (r *RepositoryPlugin) ProcessSource(ctx context.Context, request v1.ProcessSourceRequest, credentials map[string]string) (v1.ProcessSourceResponse, error) {
	// TODO implement me
	panic("implement me")
}

// func (r *RepositoryPlugin) validateEndpoint(obj runtime.Typed, jsonSchema []byte) error {
//	valid, err := plugins.ValidatePlugin(obj, jsonSchema)
//	if err != nil {
//		return fmt.Errorf("failed to validate plugin %q: %w", r.ID, err)
//	}
//	if !valid {
//		return fmt.Errorf("validation of plugin %q failed for get local resource", r.ID)
//	}
//
//	return nil
// }

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
