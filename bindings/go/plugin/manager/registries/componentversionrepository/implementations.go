package componentversionrepository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
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
	// UploadComponentVersion defines the endpoint to upload component versions to.
	UploadComponentVersion = "/component-version/upload"
	// DownloadComponentVersion defines the endpoint to download component versions.
	DownloadComponentVersion = "/component-version/download"
	// ListComponentVersions defines the endpoint to list component versions.
	ListComponentVersions = "/component-versions"
	Identity              = "/identity"
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
	_ v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed] = &RepositoryPlugin{}
)

func NewComponentVersionRepositoryPlugin(client *http.Client, id string, path string, config types.Config, loc string, jsonSchema []byte) *RepositoryPlugin {
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

func (r *RepositoryPlugin) AddComponentVersion(ctx context.Context, request v1.PostComponentVersionRequest[runtime.Typed], credentials map[string]string) error {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, UploadComponentVersion, http.MethodPost, plugins.WithPayload(request), plugins.WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to add component version with plugin %q: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) GetComponentVersion(ctx context.Context, request v1.GetComponentVersionRequest[runtime.Typed], credentials map[string]string) (*descriptor.Descriptor, error) {
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
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
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

func (r *RepositoryPlugin) ListComponentVersions(ctx context.Context, request v1.ListComponentVersionsRequest[runtime.Typed], credentials map[string]string) ([]string, error) {
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
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return nil, err
	}

	var result []string
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, ListComponentVersions, http.MethodGet, plugins.WithResult(&result), plugins.WithQueryParams(params), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component version %s from %s: %w", request.Name, r.ID, err)
	}

	return result, nil
}

func (r *RepositoryPlugin) AddLocalResource(ctx context.Context, request v1.PostLocalResourceRequest[runtime.Typed], credentials map[string]string) (*descriptor.Resource, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
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

func (r *RepositoryPlugin) GetLocalResource(ctx context.Context, request v1.GetLocalResourceRequest[runtime.Typed], credentials map[string]string) error {
	var params []plugins.KV
	addParam := func(k, v string) {
		params = append(params, plugins.KV{Key: k, Value: v})
	}
	addParam("name", request.Name)
	addParam("version", request.Version)
	addParam("target_location_type", string(request.TargetLocation.LocationType))
	addParam("target_location_value", request.TargetLocation.Value)
	identityEncoded, err := json.Marshal(request.Identity)
	if err != nil {
		return err
	}
	identityBase64 := base64.StdEncoding.EncodeToString(identityEncoded)
	addParam("identity", identityBase64)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return err
	}

	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, DownloadLocalResource, http.MethodGet, plugins.WithQueryParams(params), plugins.WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to get local resource %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	_, err = os.Stat(request.TargetLocation.Value)
	if err != nil {
		return fmt.Errorf("failed to stat target file: %w", err)
	}

	return nil
}

func (r *RepositoryPlugin) GetIdentity(ctx context.Context, typ v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	if err := r.validateEndpoint(typ.Typ, r.jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", r.ID, err)
	}

	identity := &runtime.Identity{}
	if err := plugins.Call(ctx, r.client, r.config.Type, r.location, Identity, http.MethodPost, plugins.WithPayload(typ), plugins.WithResult(identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", r.ID, err)
	}

	return nil, nil
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
