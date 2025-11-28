package componentversionrepository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Endpoints
const (
	// UploadLocalResource defines the endpoint to upload a local resource to.
	UploadLocalResource = "/local-resource/upload"
	// DownloadLocalResource defines the endpoint to download a local resource.
	DownloadLocalResource = "/local-resource/download"
	// UploadLocalSource defines the endpoint to upload a local source to.
	UploadLocalSource = "/local-source/upload"
	// DownloadLocalSource defines the endpoint to download a local source.
	DownloadLocalSource = "/local-source/download"
	// UploadComponentVersion defines the endpoint to upload component versions to.
	UploadComponentVersion = "/component-version/upload"
	// DownloadComponentVersion defines the endpoint to download component versions.
	DownloadComponentVersion = "/component-version/download"
	// ListComponentVersions defines the endpoint to list component versions.
	ListComponentVersions = "/component-versions"
	// Identity defines the endpoint to retrieve credential consumer identity.
	Identity = "/identity"
	// CheckHealth defines the endpoint to check the health of a component version repository.
	CheckHealth = "/component-version/check-health"
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

	capability ocmrepositoryv1.CapabilitySpec
	// location is where the plugin started listening.
	location string
}

// This plugin implements all the given contracts.
var (
	_ ocmrepositoryv1.ReadWriteOCMRepositoryPluginContract[runtime.Typed] = &RepositoryPlugin{}
)

// NewComponentVersionRepositoryPlugin creates a new component version repository plugin instance with the provided configuration.
// It initializes the plugin with an HTTP client, unique ID, path, configuration, location, and JSON schema.
func NewComponentVersionRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, capability ocmrepositoryv1.CapabilitySpec) *RepositoryPlugin {
	return &RepositoryPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
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

func (r *RepositoryPlugin) AddComponentVersion(ctx context.Context, request ocmrepositoryv1.PostComponentVersionRequest[runtime.Typed], credentials map[string]string) error {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, UploadComponentVersion, http.MethodPost, plugins.WithPayload(request), plugins.WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to add component version with plugin %q: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) GetComponentVersion(ctx context.Context, request ocmrepositoryv1.GetComponentVersionRequest[runtime.Typed], credentials map[string]string) (*descriptor.Descriptor, error) {
	var params []plugins.KV
	addParam := func(k, v string) {
		params = append(params, plugins.KV{Key: k, Value: v})
	}
	addParam("name", request.Name)
	addParam("version", request.Version)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return nil, err
	}

	descV2 := &v2.Descriptor{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, DownloadComponentVersion, http.MethodGet, plugins.WithResult(descV2), plugins.WithQueryParams(params), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	desc, err := descriptor.ConvertFromV2(descV2)
	if err != nil {
		return nil, fmt.Errorf("failed to convert component version descriptor: %w", err)
	}

	return desc, nil
}

func (r *RepositoryPlugin) ListComponentVersions(ctx context.Context, request ocmrepositoryv1.ListComponentVersionsRequest[runtime.Typed], credentials map[string]string) ([]string, error) {
	var params []plugins.KV
	addParam := func(k, v string) {
		params = append(params, plugins.KV{Key: k, Value: v})
	}
	addParam("name", request.Name)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return nil, err
	}

	var result []string
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ListComponentVersions, http.MethodGet, plugins.WithResult(&result), plugins.WithQueryParams(params), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component version %s from %s: %w", request.Name, r.ID, err)
	}

	return result, nil
}

func (r *RepositoryPlugin) AddLocalResource(ctx context.Context, request ocmrepositoryv1.PostLocalResourceRequest[runtime.Typed], credentials map[string]string) (*descriptor.Resource, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return nil, err
	}

	resourceV2 := &v2.Resource{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, UploadLocalResource, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(resourceV2), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add local resource %s: %w", r.ID, err)
	}

	resources := descriptor.ConvertFromV2Resources([]v2.Resource{*resourceV2})
	if len(resources) == 0 {
		return nil, errors.New("number of converted resources is zero")
	}

	return &resources[0], nil
}

func (r *RepositoryPlugin) GetLocalResource(ctx context.Context, request ocmrepositoryv1.GetLocalResourceRequest[runtime.Typed], credentials map[string]string) (ocmrepositoryv1.GetLocalResourceResponse, error) {
	var params []plugins.KV
	addParam := func(k, v string) {
		params = append(params, plugins.KV{Key: k, Value: v})
	}
	addParam("name", request.Name)
	addParam("version", request.Version)
	identityEncoded, err := json.Marshal(request.Identity)
	var response ocmrepositoryv1.GetLocalResourceResponse
	if err != nil {
		return response, err
	}
	identityBase64 := base64.StdEncoding.EncodeToString(identityEncoded)
	addParam("identity", identityBase64)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return response, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return response, err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, DownloadLocalResource, http.MethodGet, plugins.WithQueryParams(params), plugins.WithHeader(credHeader), plugins.WithResult(&response)); err != nil {
		return response, fmt.Errorf("failed to get local resource %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) AddLocalSource(ctx context.Context, request ocmrepositoryv1.PostLocalSourceRequest[runtime.Typed], credentials map[string]string) (*descriptor.Source, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return nil, err
	}

	sourceV2 := &v2.Source{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, UploadLocalSource, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(sourceV2), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add local source %s: %w", r.ID, err)
	}

	sources := descriptor.ConvertFromV2Sources([]v2.Source{*sourceV2})
	if len(sources) == 0 {
		return nil, errors.New("number of converted sources is zero")
	}

	return &sources[0], nil
}

func (r *RepositoryPlugin) GetLocalSource(ctx context.Context, request ocmrepositoryv1.GetLocalSourceRequest[runtime.Typed], credentials map[string]string) (ocmrepositoryv1.GetLocalSourceResponse, error) {
	var params []plugins.KV
	addParam := func(k, v string) {
		params = append(params, plugins.KV{Key: k, Value: v})
	}
	addParam("name", request.Name)
	addParam("version", request.Version)
	identityEncoded, err := json.Marshal(request.Identity)
	var response ocmrepositoryv1.GetLocalSourceResponse
	if err != nil {
		return response, err
	}
	identityBase64 := base64.StdEncoding.EncodeToString(identityEncoded)
	addParam("identity", identityBase64)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return response, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return response, err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, DownloadLocalSource, http.MethodGet, plugins.WithQueryParams(params), plugins.WithHeader(credHeader), plugins.WithResult(&response)); err != nil {
		return response, fmt.Errorf("failed to get local source %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, request *ocmrepositoryv1.GetIdentityRequest[runtime.Typed]) (*ocmrepositoryv1.GetIdentityResponse, error) {
	if err := r.validateEndpoint(request.Typ); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", r.ID, err)
	}

	identity := ocmrepositoryv1.GetIdentityResponse{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", r.ID, err)
	}

	return &identity, nil
}

func (r *RepositoryPlugin) CheckHealth(ctx context.Context, request ocmrepositoryv1.PostCheckHealthRequest[runtime.Typed], credentials map[string]string) error {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository); err != nil {
		return err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, CheckHealth, http.MethodPost, plugins.WithPayload(request), plugins.WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to add component version with plugin %q: %w", r.ID, err)
	}

	return nil
}

// validateEndpoint uses the provided JSON schema and the runtime.Typed and, using the JSON schema, validates that the
// underlying runtime.Type conforms to the provided schema.
// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (r *RepositoryPlugin) validateEndpoint(obj runtime.Typed) error {
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
