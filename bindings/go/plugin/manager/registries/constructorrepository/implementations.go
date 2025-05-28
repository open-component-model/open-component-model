package constructorrepositroy

import (
	"context"
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

	// jsonSchema is the schema for all endpoints for this plugin.
	jsonSchema []byte
	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ v1.ResourceInputPluginContract   = (*RepositoryPlugin)(nil)
	_ v1.SourceInputPluginContract     = (*RepositoryPlugin)(nil)
	_ v1.ResourceDigestProcessorPlugin = (*RepositoryPlugin)(nil)
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
	panic("implement me")
}

func (r *RepositoryPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceRequest, credentials map[string]string) (*v1.ProcessResourceResponse, error) {
	panic("implement me")
}

func (r *RepositoryPlugin) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource) (*descriptor.Resource, error) {
	// TODO implement me
	panic("implement me")
}

func (r *RepositoryPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceRequest, credentials map[string]string) (*v1.ProcessSourceResponse, error) {
	// TODO implement me
	panic("implement me")
}
