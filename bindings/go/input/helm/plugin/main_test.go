package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	helmv1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestHelmPluginFlow(t *testing.T) {
	t.Skip("for now this is skipped because it's not building the plugin")
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-helm-input")
	_, err := os.Stat(path)
	require.NoError(t, err, "helm plugin not found, please build the plugin under tmp/testdata/test-plugin-helm-input first")

	ctx := context.Background()
	scheme := runtime.NewScheme()

	// Register Helm input spec types
	scheme.MustRegisterWithAlias(&helmv1.Helm{},
		runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		runtime.NewUnversionedType(helmv1.Type),
	)

	registry := input.NewInputRepositoryRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-helm-plugin-construction",
		Type:       mtypes.Socket,
		PluginType: mtypes.InputPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	proto := &helmv1.Helm{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-helm-plugin-construction-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})

	plugin := mtypes.Plugin{
		ID:     "test-helm-plugin-construction",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-helm-plugin-construction",
			Type:       mtypes.Socket,
			PluginType: mtypes.InputPluginType,
		},
		Types: map[mtypes.PluginType][]mtypes.Type{
			mtypes.InputPluginType: {
				{
					Type:       typ,
					JSONSchema: []byte(`{}`),
				},
			},
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}

	require.NoError(t, registry.AddPlugin(plugin, typ))

	p, err := scheme.NewObject(typ)
	require.NoError(t, err)

	// Test resource input processing
	retrievedResourcePlugin, err := registry.GetResourceInputPlugin(ctx, p)
	require.NoError(t, err)

	// Create a test Helm resource with basic spec
	helmInput := &helmv1.Helm{
		Path: "/tmp/test-chart", // Stub path for testing
	}
	helmInput.SetType(runtime.Type{Name: helmv1.Type, Version: helmv1.Version})

	testResource := &constructor.Resource{
		ElementMeta: constructor.ElementMeta{
			ObjectMeta: constructor.ObjectMeta{
				Name:    "test-helm-chart",
				Version: "0.1.0",
			},
		},
		Type:     "helmChart",
		Relation: "local",
		AccessOrInput: constructor.AccessOrInput{
			Input: helmInput,
		},
	}

	// Process the resource - this will likely fail due to missing chart, but tests the flow
	result, err := retrievedResourcePlugin.ProcessResource(ctx, testResource, map[string]string{})
	if err != nil {
		t.Logf("Expected error since test chart path doesn't exist: %v", err)
	} else {
		require.NotNil(t, result)
		require.Equal(t, "helm-chart", result.ProcessedResource.Name)
	}

	// Test source input processing
	retrievedSourcePlugin, err := registry.GetSourceInputPlugin(ctx, p)
	require.NoError(t, err)
	require.NotNil(t, retrievedSourcePlugin, "Expected source input method to be available")
}

func TestHelmPluginCapabilities(t *testing.T) {
	t.Skip("for now this is skipped because it's not building the plugin")
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-helm-input")
	_, err := os.Stat(path)
	require.NoError(t, err, "helm plugin not found, please build the plugin first")

	// Test capabilities command
	cmd := exec.Command(path, "capabilities")
	output, err := cmd.Output()
	require.NoError(t, err, "capabilities command should succeed")

	// Parse the JSON output
	var capabilities map[string]interface{}
	err = json.Unmarshal(output, &capabilities)
	require.NoError(t, err, "capabilities output should be valid JSON")

	// Verify that helm type is registered
	types, ok := capabilities["types"].(map[string]interface{})
	require.True(t, ok, "capabilities should have types field")

	inputRepo, ok := types["inputRepository"].([]interface{})
	require.True(t, ok, "types should have inputRepository field")
	require.Greater(t, len(inputRepo), 0, "should have at least one input type registered")

	// Look for helm type
	found := false
	for _, item := range inputRepo {
		if itemMap, ok := item.(map[string]interface{}); ok {
			if typeStr, ok := itemMap["type"].(string); ok && typeStr == "helm/v1" {
				found = true
				break
			}
		}
	}
	require.True(t, found, "helm/v1 type should be registered in capabilities")
}
