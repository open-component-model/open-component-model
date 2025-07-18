package blobtransformer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Endpoints
const (
	// TransformBlob defines the endpoint to transform a blob.
	TransformBlob = "/blob/transform"
	// Identity defines the endpoint to retrieve credential consumer identity.
	Identity = "/identity"
)

// RepositoryPlugin implements the ReadWriteOCMRepositoryPluginContract for external plugin communication.
// It handles REST-based communication with external repository plugins, including request validation,
// credential management, and data format conversion.
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
	_ v1.BlobTransformerPluginContract[runtime.Typed] = &RepositoryPlugin{}
)

// NewPlugin creates a new plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and JSON schema.
func NewPlugin(client *http.Client, id string, path string, config types.Config, loc string, jsonSchema []byte) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		jsonSchema: jsonSchema,
		location:   loc,
	}
}

func (r *RepositoryPlugin) TransformBlob(ctx context.Context, request *v1.TransformBlobRequest[runtime.Typed], credentials map[string]string) (*v1.TransformBlobResponse, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Specification, r.jsonSchema); err != nil {
		return nil, err
	}

	response := &v1.TransformBlobResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, TransformBlob, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to transform blob via %s: %w", r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", r.ID)

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, request *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	if err := r.validateEndpoint(request.Typ, r.jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", r.ID, err)
	}

	identity := v1.GetIdentityResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", r.ID, err)
	}

	return &identity, nil
}

// validateEndpoint uses the provided JSON schema and the runtime.Typed and, using the JSON schema, validates that the
// underlying runtime.Type conforms to the provided schema.
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
