package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryPlugin implements the resourcev1.ReadWriteResourceRepositoryPluginContract interface.
type RepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	capability resourcev1.CapabilitySpec

	// location is where the plugin started listening.
	location string
}

var _ resourcev1.ReadWriteResourcePluginContract = (*RepositoryPlugin)(nil)

// NewResourceRepositoryPlugin creates a new RepositoryPlugin.
func NewResourceRepositoryPlugin(
	client *http.Client,
	id string,
	path string,
	config types.Config,
	location string,
	capability resourcev1.CapabilitySpec,
) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   location,
	}
}

func (p *RepositoryPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", p.ID)

	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", p.ID, err)
	}

	return nil
}

func (p *RepositoryPlugin) GetIdentity(ctx context.Context, request *resourcev1.GetIdentityRequest[runtime.Typed]) (*resourcev1.GetIdentityResponse, error) {
	if err := p.validateEndpoint(request.Typ); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	identity := resourcev1.GetIdentityResponse{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, GetIdentity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", p.ID, err)
	}

	return &identity, nil
}

// GetGlobalResource retrieves a global resource.
func (p *RepositoryPlugin) GetGlobalResource(ctx context.Context, req *resourcev1.GetGlobalResourceRequest, credentials map[string]string) (*resourcev1.GetGlobalResourceResponse, error) {
	if err := p.validateEndpoint(req.Resource.Access); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	var response resourcev1.GetGlobalResourceResponse
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, GetGlobalResource, http.MethodPost, plugins.WithPayload(req), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to get global resource from plugin %q: %w", p.ID, err)
	}
	return &response, nil
}

// AddGlobalResource adds a global resource.
func (p *RepositoryPlugin) AddGlobalResource(ctx context.Context, req *resourcev1.AddGlobalResourceRequest, credentials map[string]string) (*resourcev1.AddGlobalResourceResponse, error) {
	if err := p.validateEndpoint(req.Resource.Access); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	var response resourcev1.AddGlobalResourceResponse
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, AddGlobalResource, http.MethodPost, plugins.WithPayload(req), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add global resource to plugin %q: %w", p.ID, err)
	}

	return &response, nil
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
