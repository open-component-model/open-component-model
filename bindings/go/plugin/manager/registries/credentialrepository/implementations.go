package credentialrepository

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
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
	_ v1.CredentialRepositoryPluginContract[runtime.Typed] = &RepositoryPlugin{}
)

func NewCredentialRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, jsonSchema []byte) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		jsonSchema: jsonSchema,
		location:   loc,
	}
}

func (r RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", r.ID)

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

// TODO Implement
func (r RepositoryPlugin) ConsumerIdentityForConfig(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
	// TODO implement me
	panic("implement me")
}

// TODO Implement
func (r RepositoryPlugin) Resolve(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials map[string]string) (map[string]string, error) {
	// TODO implement me
	panic("implement me")
}
