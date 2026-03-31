package clicommand

import (
	"encoding/json"
	"fmt"
	"net/http"

	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RegisterCLICommand registers a CLI command handler with the EndpointBuilder.
// Call this from your plugin binary's main() before marshalling capabilities.
//
// Example:
//
//	if err := clicommand.RegisterCLICommand(
//	    clicommandv1.CommandSpec{Verb: "publish", ObjectType: "rbsc", Short: "…"},
//	    &myPlugin{},
//	    caps,
//	); err != nil {
//	    log.Fatal(err)
//	}
func RegisterCLICommand(
	spec clicommandv1.CommandSpec,
	handler clicommandv1.CLICommandPluginContract,
	c *endpoints.EndpointBuilder,
) error {
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Location: endpointGetCredentialConsumerIdentity,
			Handler:  handleGetCredentialConsumerIdentity(handler),
		},
		endpoints.Handler{
			Location: endpointExecute,
			Handler:  handleExecute(handler),
		},
	)

	// Merge with existing cliCommand capability spec if one already exists,
	// so a single binary can register multiple commands.
	for i, existing := range c.PluginSpec.CapabilitySpecs {
		if cap, ok := existing.(*clicommandv1.CapabilitySpec); ok {
			cap.SupportedCommands = append(cap.SupportedCommands, spec)
			c.PluginSpec.CapabilitySpecs[i] = cap
			return nil
		}
	}

	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &clicommandv1.CapabilitySpec{
		Type:              runtime.NewUnversionedType(string(clicommandv1.CLICommandPluginType)),
		SupportedCommands: []clicommandv1.CommandSpec{spec},
	})

	return nil
}

// handleGetCredentialConsumerIdentity is the HTTP handler for the identity endpoint.
func handleGetCredentialConsumerIdentity(plugin clicommandv1.CLICommandPluginContract) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req clicommandv1.GetCredentialConsumerIdentityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			plugins.NewError(fmt.Errorf("failed to decode request: %w", err), http.StatusBadRequest).Write(w)
			return
		}

		resp, err := plugin.GetCLICommandCredentialConsumerIdentity(r.Context(), &req)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(w)
			return
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(w)
		}
	}
}

// handleExecute is the HTTP handler for the execute endpoint.
func handleExecute(plugin clicommandv1.CLICommandPluginContract) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract credentials from Authorization header (JSON-encoded map).
		var credentials map[string]string
		if auth := r.Header.Get("Authorization"); auth != "" {
			if err := json.Unmarshal([]byte(auth), &credentials); err != nil {
				plugins.NewError(fmt.Errorf("malformed Authorization header: %w", err), http.StatusUnauthorized).Write(w)
				return
			}
		}

		var req clicommandv1.ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			plugins.NewError(fmt.Errorf("failed to decode request: %w", err), http.StatusBadRequest).Write(w)
			return
		}

		resp, err := plugin.Execute(r.Context(), &req, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(w)
			return
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(w)
		}
	}
}
