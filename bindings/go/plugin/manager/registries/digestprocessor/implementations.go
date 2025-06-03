package digestprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type DigestProcessorPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config mtypes.Config
	path   string
	client *http.Client

	// jsonSchema is the schema for all endpoints for this plugin.
	jsonSchema []byte

	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ v1.ResourceDigestProcessorPlugin = (*DigestProcessorPlugin)(nil)
)

func NewDigestProcessorPlugin(client *http.Client, id string, path string, config mtypes.Config, loc string, jsonSchema []byte) *DigestProcessorPlugin {
	return &DigestProcessorPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		jsonSchema: jsonSchema,
		location:   loc,
	}
}

func (p *DigestProcessorPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", p.ID)

	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", p.ID, err)
	}

	return nil
}

func (p *DigestProcessorPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	if err := p.validateEndpoint(typ.Typ, p.jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	identity := runtime.Identity{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "GetIdentity", http.MethodPost, plugins.WithPayload(typ), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", p.ID, err)
	}

	return identity, nil
}

func (p *DigestProcessorPlugin) ProcessResourceDigest(ctx context.Context, resource descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	// Note: We don't validate the resource here since it doesn't implement runtime.Typed
	// The validation should be handled by the plugin itself

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	req := struct {
		Resource descriptor.Resource `json:"resource"`
	}{
		Resource: resource,
	}

	var result descriptor.Resource
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "ProcessResourceDigest", http.MethodPost, plugins.WithPayload(req), plugins.WithResult(&result), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to process resource digest %s: %w", p.ID, err)
	}

	return &result, nil
}

func (p *DigestProcessorPlugin) validateEndpoint(obj runtime.Typed, jsonSchema []byte) error {
	valid, err := plugins.ValidatePlugin(obj, jsonSchema)
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
