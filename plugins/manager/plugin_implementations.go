package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	XOCMRepositoryHeader = "X-OCM-Repository"
)

// Endpoints
const (
	// UploadLocalResource defines the endpoint to upload a local resource to.
	UploadLocalResource = "/local-resource/upload"
	// DownloadLocalResource defines the endpoint to download a local resource.
	DownloadLocalResource = "/local-resource/download"
	// UploadResource defines the endpoint to upload a global resource to.
	UploadResource = "/resource/upload"
	// DownloadResource defines the endpoint to download a global resource.
	DownloadResource = "/resource/download"
	// UploadComponentVersion defines the endpoint to upload component versions to.
	UploadComponentVersion = "/component-version/upload"
	// DownloadComponentVersion defines the endpoint to download component versions.
	DownloadComponentVersion = "/component-version/download"
)

type RepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config Config
	mu     sync.Mutex
	path   string
	client *http.Client
	logger *slog.Logger

	baseCtx context.Context
}

// This plugin implements all the given contracts.
var (
	_ ReadRepositoryPluginContract  = &RepositoryPlugin{}
	_ WriteRepositoryPluginContract = &RepositoryPlugin{}
	_ ReadResourcePluginContract    = &RepositoryPlugin{}
	_ WriteResourcePluginContract   = &RepositoryPlugin{}
	//_ CredentialRepositoryPluginContract = &RepositoryPlugin{}
	//_ TransformerPluginContract          = &RepositoryPlugin{}
)

func NewRepositoryPlugin(baseCtx context.Context, logger *slog.Logger, client *http.Client, id string, path string, config Config) *RepositoryPlugin {
	return &RepositoryPlugin{
		baseCtx: baseCtx,
		ID:      id,
		path:    path,
		config:  config,
		logger:  logger,
		client:  client,
	}
}

func (r *RepositoryPlugin) Ping(ctx context.Context) error {
	r.logger.Info("Pinging plugin", "id", r.ID)

	if err := call(ctx, r.client, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) AddComponentVersion(ctx context.Context, request PostComponentVersionRequest, credentials Attributes) error {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	if err := call(ctx, r.client, UploadComponentVersion, http.MethodPost, WithPayload(request), WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to add component version with plugin %q: %w", r.ID, err)
	}

	return nil
}

func (r *RepositoryPlugin) GetComponentVersion(ctx context.Context, request GetComponentVersionRequest, credentials Attributes) (*descriptor.Descriptor, error) {
	response := &descriptor.Descriptor{}
	var params []KV
	addParam := func(k, v string) {
		params = append(params, KV{Key: k, Value: v})
	}
	addParam("name", request.Name)
	addParam("version", request.Version)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	repoHeader, err := toOCMRepoHeader(request.Repository)
	if err != nil {
		return nil, err
	}

	if err := call(ctx, r.client, DownloadComponentVersion, http.MethodGet, WithResult(response), WithQueryParams(params), WithHeader(credHeader), WithHeader(repoHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) AddLocalResource(ctx context.Context, request PostLocalResourceRequest, credentials Attributes) (*descriptor.Resource, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	response := &descriptor.Resource{}
	if err := call(ctx, r.client, UploadLocalResource, http.MethodPost, WithPayload(request), WithResult(response), WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add local resource %s: %w", r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) GetLocalResource(ctx context.Context, request GetLocalResourceRequest, credentials Attributes) error {
	var params []KV
	addParam := func(k, v string) {
		params = append(params, KV{Key: k, Value: v})
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

	repoHeader, err := toOCMRepoHeader(request.Repository)
	if err != nil {
		return err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	if err := call(ctx, r.client, DownloadLocalResource, http.MethodGet, WithQueryParams(params), WithHeader(credHeader), WithHeader(repoHeader)); err != nil {
		return fmt.Errorf("failed to get local resource %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	_, err = os.Stat(request.TargetLocation.Value)
	if err != nil {
		return fmt.Errorf("failed to stat target file: %w", err)
	}

	return nil
}

func (r *RepositoryPlugin) AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials Attributes) (*descriptor.Resource, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	response := &descriptor.Resource{}
	if err := call(ctx, r.client, UploadResource, http.MethodPost, WithPayload(request), WithResult(response), WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add resource %s: %w", r.ID, err)
	}

	return response, nil
}

func (r *RepositoryPlugin) GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials Attributes) error {
	var params []KV
	addParam := func(k, v string) {
		params = append(params, KV{Key: k, Value: v})
	}
	addParam("target_location_type", string(request.TargetLocation.LocationType))
	addParam("target_location_value", request.TargetLocation.Value)
	resourceEncoded, err := json.Marshal(request.Resource)
	if err != nil {
		return err
	}
	resourceBase64 := base64.StdEncoding.EncodeToString(resourceEncoded)
	addParam("resource", resourceBase64)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	if err := call(ctx, r.client, DownloadResource, http.MethodGet, WithQueryParams(params), WithHeader(credHeader)); err != nil {
		return fmt.Errorf("failed to get local resource version %s: %w", r.ID, err)
	}

	_, err = os.Stat(request.TargetLocation.Value)
	if err != nil {
		return fmt.Errorf("failed to stat target file: %w", err)
	}

	return nil
}

// Call will use the plugin's constructed connection client to make a call to the specified
// endpoint. The result will be marshalled into the provided response if not nil.
func (r *RepositoryPlugin) Call(ctx context.Context, endpoint, method string, payload, response any, headers []KV, params []KV) error {
	return call(
		ctx,
		r.client,
		endpoint,
		method,
		WithPayload(payload),
		WithResult(response),
		WithHeaders(headers),
		WithQueryParams(params),
	)
}

func (r *RepositoryPlugin) SupportedRepositoryConfigTypes(ctx context.Context) ([]runtime.Type, error) {
	var configTypes []runtime.Type

	if err := call(ctx, r.client, "supported-credential-repository-config-types", http.MethodGet, WithResult(&configTypes)); err != nil {
		return nil, fmt.Errorf("failed to retrieve identity for repository configuration %s: %w", r.ID, err)
	}

	return configTypes, nil
}

func (r *RepositoryPlugin) ConsumerIdentityForRepositoryConfig(ctx context.Context, config runtime.Typed) (runtime.Identity, error) {
	var params []KV
	addParam := func(k, v string) {
		params = append(params, KV{Key: k, Value: v})
	}
	configEncoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	resourceBase64 := base64.StdEncoding.EncodeToString(configEncoded)
	addParam("repository_config", resourceBase64)

	identity := runtime.Identity{}

	if err := call(ctx, r.client, "consumer-identity-for-repository-config", http.MethodGet, WithQueryParams(params), WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to retrieve identity for repository configuration %s: %w", r.ID, err)
	}

	return identity, nil
}

func (r *RepositoryPlugin) Resolve(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials Attributes) (Attributes, error) {
	var params []KV
	addParam := func(k, v string) {
		params = append(params, KV{Key: k, Value: v})
	}

	configEncoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	resourceBase64 := base64.StdEncoding.EncodeToString(configEncoded)
	addParam("repository_config", resourceBase64)

	identityEncoded, err := json.Marshal(identity)
	if err != nil {
		return nil, err
	}
	identityBase64 := base64.StdEncoding.EncodeToString(identityEncoded)
	addParam("identity", identityBase64)

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	resolved := Attributes{}

	if err := call(ctx, r.client, "resolve-repository-credentials", http.MethodGet, WithQueryParams(params), WithResult(&resolved), WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to retrieve identity for repository configuration %s: %w", r.ID, err)
	}

	return resolved, nil
}

//
//func (r *RepositoryPlugin) Transform(ctx context.Context, request TransformResourceRequest) (*TransformResourceResponse, error) {
//	response := TransformResourceResponse{}
//	if err := call(ctx, r.client, "resources/transform", http.MethodPost, WithPayload(request), WithResult(&response)); err != nil {
//		return nil, fmt.Errorf("failed to transform resource with plugin %s: %w", r.ID, err)
//	}
//
//	return &response, nil
//}
//
//func (r *RepositoryPlugin) CredentialIdentities(ctx context.Context, request CredentialIdentityRequest) (*CredentialIdentityResponse, error) {
//	response := CredentialIdentityResponse{}
//	if err := call(ctx, r.client, "resources/transform/credential-identities", http.MethodPost, WithPayload(request), WithResult(&response)); err != nil {
//		return nil, fmt.Errorf("failed to transform resource with plugin %s: %w", r.ID, err)
//	}
//
//	return &response, nil
//}

func toCredentials(credentials Attributes) (KV, error) {
	rawCreds, err := json.Marshal(credentials)
	if err != nil {
		return KV{}, err
	}
	return KV{
		Key:   "Authorization",
		Value: string(rawCreds),
	}, nil
}

func toOCMRepoHeader(repository runtime.Typed) (KV, error) {
	raw, err := json.Marshal(repository)
	if err != nil {
		return KV{}, err
	}
	return KV{
		Key:   XOCMRepositoryHeader,
		Value: string(raw),
	}, nil
}
