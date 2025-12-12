package signinghandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	signingv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SigningHandlerPlugin implements the v1.SignatureHandlerContract interface.
type SigningHandlerPlugin struct {
	ID string

	// config is used to start the plugin during a later phase.
	config types.Config
	path   string
	client *http.Client

	capability signingv1.CapabilitySpec

	// location is where the plugin started listening.
	location string
}

var _ signingv1.SignatureHandlerContract[runtime.Typed] = (*SigningHandlerPlugin)(nil)

// NewSigningHandlerPlugin creates a new SigningHandlerPlugin.
func NewSigningHandlerPlugin(
	client *http.Client,
	id string,
	path string,
	config types.Config,
	location string,
	capability signingv1.CapabilitySpec,
) *SigningHandlerPlugin {
	return &SigningHandlerPlugin{
		ID:         id,
		path:       path,
		config:     config,
		client:     client,
		capability: capability,
		location:   location,
	}
}

func (p *SigningHandlerPlugin) Ping(ctx context.Context) error {
	slog.InfoContext(ctx, "Pinging plugin", "id", p.ID)

	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping plugin %s: %w", p.ID, err)
	}

	return nil
}

func (p *SigningHandlerPlugin) GetSignerIdentity(ctx context.Context, request *signingv1.GetSignerIdentityRequest[runtime.Typed]) (*signingv1.IdentityResponse, error) {
	if err := p.validateEndpoint(request.Config); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	identity := signingv1.IdentityResponse{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, GetSignerIdentity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", p.ID, err)
	}

	return &identity, nil
}

func (p *SigningHandlerPlugin) GetVerifierIdentity(ctx context.Context, request *signingv1.GetVerifierIdentityRequest[runtime.Typed]) (*signingv1.IdentityResponse, error) {
	if err := p.validateEndpoint(request.Config); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	identity := signingv1.IdentityResponse{}
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, GetVerifierIdentity, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&identity)); err != nil {
		return nil, fmt.Errorf("failed to get identity from plugin %q: %w", p.ID, err)
	}

	return &identity, nil
}

func (p *SigningHandlerPlugin) Sign(ctx context.Context, request *signingv1.SignRequest[runtime.Typed], credentials map[string]string) (*signingv1.SignResponse, error) {
	if err := p.validateEndpoint(request.Config); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	var response signingv1.SignResponse
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, Sign, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to sign from plugin %q: %w", p.ID, err)
	}

	return &response, nil
}

func (p *SigningHandlerPlugin) Verify(ctx context.Context, request *signingv1.VerifyRequest[runtime.Typed], credentials map[string]string) (*signingv1.VerifyResponse, error) {
	if err := p.validateEndpoint(request.Config); err != nil {
		return nil, fmt.Errorf("failed to validate type %q: %w", p.ID, err)
	}

	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error converting credentials: %w", err)
	}

	var response signingv1.VerifyResponse
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, Verify, http.MethodPost, plugins.WithPayload(request), plugins.WithResult(&response), plugins.WithHeader(credHeader)); err != nil {
		return nil, fmt.Errorf("failed to verify from plugin %q: %w", p.ID, err)
	}

	return &response, nil
}

// TODO(fabianburth): this method looks essentially the same for all plugin make it reusable!
func (p *SigningHandlerPlugin) validateEndpoint(obj runtime.Typed) error {
	var schema []byte
	for _, t := range p.capability.SupportedSigningSpecTypes {
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
