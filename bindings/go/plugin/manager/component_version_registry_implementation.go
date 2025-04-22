// Package manager this implementation only deals with component version registry implementations.
package manager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/santhosh-tekuri/jsonschema/v6"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	XOCMRepositoryHeader = "X-Ocm-Repository"
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
)

func NewTypedComponentVersionRepositoryPluginImplementation[T runtime.Typed](base *ComponentVersionRepositoryPlugin) *TypedComponentVersionRepositoryPlugin[T] {
	return &TypedComponentVersionRepositoryPlugin[T]{base}
}

type TypedComponentVersionRepositoryPlugin[T runtime.Typed] struct {
	base *ComponentVersionRepositoryPlugin
}

func (r *TypedComponentVersionRepositoryPlugin[T]) GetLocalResource(ctx context.Context, request GetLocalResourceRequest[T], credentials Attributes) error {
	return r.base.GetLocalResource(ctx, GetLocalResourceRequest[runtime.Typed]{
		Repository:     request.Repository,
		Name:           request.Name,
		Version:        request.Version,
		Identity:       request.Identity,
		TargetLocation: request.TargetLocation,
	}, credentials)
}

func (r *TypedComponentVersionRepositoryPlugin[T]) AddLocalResource(ctx context.Context, request PostLocalResourceRequest[T], credentials Attributes) (*descriptor.Resource, error) {
	return r.base.AddLocalResource(ctx, PostLocalResourceRequest[runtime.Typed]{
		Repository:       request.Repository,
		Name:             request.Name,
		Version:          request.Version,
		ResourceLocation: request.ResourceLocation,
		Resource:         request.Resource,
	}, credentials)
}
func (r *TypedComponentVersionRepositoryPlugin[T]) Ping(ctx context.Context) error {
	return r.base.Ping(ctx)
}

func (r *TypedComponentVersionRepositoryPlugin[T]) AddComponentVersion(ctx context.Context, request PostComponentVersionRequest[T], credentials Attributes) error {
	return r.base.AddComponentVersion(ctx, PostComponentVersionRequest[runtime.Typed]{
		Repository: request.Repository,
		Descriptor: request.Descriptor,
	}, credentials)
}

func (r *TypedComponentVersionRepositoryPlugin[T]) GetComponentVersion(ctx context.Context, request GetComponentVersionRequest[T], credentials Attributes) (*descriptor.Descriptor, error) {
	req := GetComponentVersionRequest[runtime.Typed]{
		Name:       request.Name,
		Version:    request.Version,
		Repository: request.Repository,
	}
	return r.base.GetComponentVersion(ctx, req, credentials)
}

type ComponentVersionRepositoryPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config Config
	path   string
	client *http.Client
	logger *slog.Logger

	baseCtx context.Context

	// jsonSchema is the schema for all endpoints for this plugin.
	jsonSchema []byte
}

// This plugin implements all the given contracts.
var (
	_ ReadWriteOCMRepositoryPluginContract[runtime.Typed] = &ComponentVersionRepositoryPlugin{}
)

func NewComponentVersionRepositoryPlugin(baseCtx context.Context, logger *slog.Logger, client *http.Client, id string, path string, config Config, jsonSchema []byte) *ComponentVersionRepositoryPlugin {
	return &ComponentVersionRepositoryPlugin{
		baseCtx:    baseCtx,
		ID:         id,
		path:       path,
		config:     config,
		logger:     logger,
		client:     client,
		jsonSchema: jsonSchema,
	}
}

func (r *ComponentVersionRepositoryPlugin) Ping(ctx context.Context) error {
	r.logger.Info("Pinging plugin", "id", r.ID)

	if err := call(ctx, r.client, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", r.ID, err)
	}

	return nil
}

func (r *ComponentVersionRepositoryPlugin) AddComponentVersion(ctx context.Context, request PostComponentVersionRequest[runtime.Typed], credentials Attributes) error {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return err
	}

	if err := call(ctx, r.client, UploadComponentVersion, http.MethodPost, WithPayload(request), WithHeader(credHeader)); err != nil {
		// TODO:
		return fmt.Errorf("failed to add component version with plugin %q: %w", r.ID, err)
	}

	return nil
}

func (r *ComponentVersionRepositoryPlugin) GetComponentVersion(ctx context.Context, request GetComponentVersionRequest[runtime.Typed], credentials Attributes) (*descriptor.Descriptor, error) {
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

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return nil, err
	}

	if err := call(ctx, r.client, DownloadComponentVersion, http.MethodGet, WithResult(response), WithQueryParams(params), WithHeader(credHeader), WithHeader(repoHeader)); err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s from %s: %w", request.Name, request.Version, r.ID, err)
	}

	return response, nil
}

func (r *ComponentVersionRepositoryPlugin) AddLocalResource(ctx context.Context, request PostLocalResourceRequest[runtime.Typed], credentials Attributes) (*descriptor.Resource, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
		return nil, err
	}

	response := &descriptor.Resource{}
	if err := call(ctx, r.client, UploadLocalResource, http.MethodPost, WithPayload(request), WithResult(response), WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to add local resource %s: %w", r.ID, err)
	}

	return response, nil
}

func (r *ComponentVersionRepositoryPlugin) GetLocalResource(ctx context.Context, request GetLocalResourceRequest[runtime.Typed], credentials Attributes) error {
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

	repoHeader, err := toOCMRepoHeader(request.Repository) // Raw
	if err != nil {
		return err
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return err
	}

	// We know we only have this single schema for all endpoints which require validation.
	if err := r.validateEndpoint(request.Repository, r.jsonSchema); err != nil {
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

// Call will use the plugin's constructed connection client to make a call to the specified
// endpoint. The result will be marshalled into the provided response if not nil.
func (r *ComponentVersionRepositoryPlugin) Call(ctx context.Context, endpoint, method string, payload, response any, headers []KV, params []KV) error {
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

func (r *ComponentVersionRepositoryPlugin) validateEndpoint(obj runtime.Typed, jsonSchema []byte) error {
	valid, err := validatePlugin(obj, jsonSchema)
	if err != nil {
		return fmt.Errorf("failed to validate plugin %q: %w", r.ID, err)
	}
	if !valid {
		return fmt.Errorf("validation of plugin %q failed for get local resource", r.ID)
	}

	return nil
}

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

func validatePlugin(typ runtime.Typed, jsonSchema []byte) (bool, error) {
	c := jsonschema.NewCompiler()
	unmarshaler, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonSchema))
	var v any
	if err := json.Unmarshal(jsonSchema, &v); err != nil {
		return true, err
	}

	if err := c.AddResource("schema.json", unmarshaler); err != nil {
		return true, fmt.Errorf("failed to add schema.json: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return true, fmt.Errorf("failed to compile schema.json: %w", err)
	}

	// need to marshal the interface into a JSON format.
	content, err := json.Marshal(typ)
	if err != nil {
		return true, fmt.Errorf("failed to marshal type: %w", err)
	}
	// once marshalled, we create a map[string]any representation of the marshaled content.
	unmarshalledType, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return true, fmt.Errorf("failed to unmarshal : %w", err)
	}

	if _, ok := unmarshalledType.(string); ok {
		// TODO: In _not_ POC this should be either a type switch, or some kind of exclusion or we should change how
		// we register and look up plugins to avoid validating when listing or for certain plugins.
		// skip validation if the passed in type is of type string.
		return true, nil
	}

	// finally, validate map[string]any against the loaded schema
	if err := sch.Validate(unmarshalledType); err != nil {
		var typRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&typRaw, content, "", "  "))
		var schemaRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&schemaRaw, jsonSchema, "", "  "))
		return true, fmt.Errorf("failed to validate schema for\n%s\n---SCHEMA---\n%s\n: %w", typRaw.String(), schemaRaw.String(), err)
	}

	return true, nil
}
