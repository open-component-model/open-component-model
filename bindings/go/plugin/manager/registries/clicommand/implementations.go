package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// Endpoint paths.
const (
	endpointGetCredentialConsumerIdentity = "/clicommand/identity"
	endpointExecute                       = "/clicommand/execute"
)

// cliCommandPlugin is the external (HTTP-based) implementation of CLICommandPluginContract.
type cliCommandPlugin struct {
	id         string
	config     types.Config
	client     *http.Client
	location   string
	capability clicommandv1.CapabilitySpec
}

var _ clicommandv1.CLICommandPluginContract = (*cliCommandPlugin)(nil)

func newCLICommandPlugin(
	client *http.Client,
	id string,
	config types.Config,
	location string,
	capability clicommandv1.CapabilitySpec,
) *cliCommandPlugin {
	return &cliCommandPlugin{
		id:         id,
		config:     config,
		client:     client,
		location:   location,
		capability: capability,
	}
}

func (p *cliCommandPlugin) Ping(ctx context.Context) error {
	if err := plugins.Call(ctx, p.client, p.config.Type, p.location, "healthz", http.MethodGet); err != nil {
		return fmt.Errorf("failed to ping CLI command plugin %s: %w", p.id, err)
	}
	return nil
}

func (p *cliCommandPlugin) GetCLICommandCredentialConsumerIdentity(
	ctx context.Context,
	req *clicommandv1.GetCredentialConsumerIdentityRequest,
) (*clicommandv1.GetCredentialConsumerIdentityResponse, error) {
	resp := &clicommandv1.GetCredentialConsumerIdentityResponse{}
	if err := plugins.Call(
		ctx, p.client, p.config.Type, p.location,
		endpointGetCredentialConsumerIdentity, http.MethodPost,
		plugins.WithPayload(req),
		plugins.WithResult(resp),
	); err != nil {
		return nil, fmt.Errorf("failed to get credential consumer identity from plugin %s: %w", p.id, err)
	}
	return resp, nil
}

func (p *cliCommandPlugin) Execute(
	ctx context.Context,
	req *clicommandv1.ExecuteRequest,
	credentials map[string]string,
) (*clicommandv1.ExecuteResponse, error) {
	credHeader, err := toCredentials(credentials)
	if err != nil {
		return nil, err
	}

	resp := &clicommandv1.ExecuteResponse{}
	if err := plugins.Call(
		ctx, p.client, p.config.Type, p.location,
		endpointExecute, http.MethodPost,
		plugins.WithPayload(req),
		plugins.WithResult(resp),
		plugins.WithHeader(credHeader),
	); err != nil {
		return nil, fmt.Errorf("failed to execute CLI command via plugin %s: %w", p.id, err)
	}
	return resp, nil
}

func toCredentials(credentials map[string]string) (plugins.KV, error) {
	raw, err := json.Marshal(credentials)
	if err != nil {
		return plugins.KV{}, err
	}
	return plugins.KV{Key: "Authorization", Value: string(raw)}, nil
}
