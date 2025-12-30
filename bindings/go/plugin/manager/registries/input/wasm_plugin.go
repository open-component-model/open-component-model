package input

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	extism "github.com/extism/go-sdk"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// WasmInputPlugin implements InputPluginContract using Extism to execute Wasm modules.
type WasmInputPlugin struct {
	plugin *extism.Plugin
	id     string
	path   string
}

// NewWasmInputPlugin creates a new Wasm-based input plugin.
func NewWasmInputPlugin(ctx context.Context, wasmPath string, pluginID string) (*WasmInputPlugin, error) {
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read wasm file: %w", err)
	}

	manifest := extism.Manifest{
		Wasm: []extism.Wasm{
			extism.WasmData{
				Data: wasmBytes,
			},
		},
		AllowedHosts: []string{"*"},
		AllowedPaths: map[string]string{
			// TODO: This should be configured by the filepath setting.
			//"*": "/tmp",
		},
		Config: map[string]string{},
	}

	config := extism.PluginConfig{
		EnableWasi: true,
	}

	plugin, err := extism.NewPlugin(ctx, manifest, config, []extism.HostFunction{})
	if err != nil {
		return nil, fmt.Errorf("failed to create extism plugin: %w", err)
	}

	return &WasmInputPlugin{
		plugin: plugin,
		id:     pluginID,
		path:   wasmPath,
	}, nil
}

// Ping checks if the plugin is responsive.
func (w *WasmInputPlugin) Ping(_ context.Context) error {
	return nil
}

// GetIdentity returns identity information for the plugin.
func (w *WasmInputPlugin) GetIdentity(_ context.Context, _ *inputv1.GetIdentityRequest[runtime.Typed]) (*inputv1.GetIdentityResponse, error) {
	return &inputv1.GetIdentityResponse{
		Identity: map[string]string{},
	}, nil
}

// ProcessResource processes a resource input request via the Wasm plugin.
func (w *WasmInputPlugin) ProcessResource(ctx context.Context, request *inputv1.ProcessResourceInputRequest, credentials map[string]string) (*inputv1.ProcessResourceInputResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	_, output, err := w.plugin.Call("process_resource", requestJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to call wasm function: %w", err)
	}

	var response inputv1.ProcessResourceInputResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// ProcessSource processes a source input request via the Wasm plugin.
func (w *WasmInputPlugin) ProcessSource(ctx context.Context, request *inputv1.ProcessSourceInputRequest, credentials map[string]string) (*inputv1.ProcessSourceInputResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	_, output, err := w.plugin.Call("process_source", requestJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to call wasm function: %w", err)
	}

	var response inputv1.ProcessSourceInputResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// Close cleans up the Wasm plugin resources.
func (w *WasmInputPlugin) Close() error {
	w.plugin.Close(context.Background())
	return nil
}
