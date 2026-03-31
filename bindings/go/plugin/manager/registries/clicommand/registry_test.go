package clicommand

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-clicommand")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build it under tmp/testdata/test-plugin-clicommand first")

	ctx := context.Background()
	registry := NewCLICommandRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-clicommand",
		Type:       mtypes.Socket,
		PluginType: clicommandv1.CLICommandPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-clicommand-plugin.socket")
		if pluginCmd.Process != nil {
			_ = pluginCmd.Process.Kill()
		}
	})

	plug := mtypes.Plugin{
		ID:     "test-plugin-clicommand",
		Path:   path,
		Stderr: stderr,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := clicommandv1.CapabilitySpec{
		Type: clicommandv1.CapabilitySpec{}.Type,
		SupportedCommands: []clicommandv1.CommandSpec{
			{Verb: "greet", ObjectType: "hello", Short: "Say hello"},
		},
	}
	require.NoError(t, registry.AddPlugin(plug, &capability))

	contract, err := registry.GetPlugin(ctx, "greet", "hello")
	require.NoError(t, err)

	// Verify credential consumer identity.
	identResp, err := contract.GetCLICommandCredentialConsumerIdentity(ctx, &clicommandv1.GetCredentialConsumerIdentityRequest{
		Verb:       "greet",
		ObjectType: "hello",
	})
	require.NoError(t, err)
	require.Equal(t, "test", identResp.Identity["service"])

	// Execute command.
	execResp, err := contract.Execute(ctx, &clicommandv1.ExecuteRequest{
		Verb:       "greet",
		ObjectType: "hello",
		Flags:      map[string]string{"name": "ocm"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "hello, ocm!\n", execResp.Output)
}

func TestShutdown(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-clicommand")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build it under tmp/testdata/test-plugin-clicommand first")

	ctx := context.Background()
	registry := NewCLICommandRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-clicommand",
		Type:       mtypes.Socket,
		PluginType: clicommandv1.CLICommandPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-clicommand-plugin.socket")
		if pluginCmd.Process != nil {
			_ = pluginCmd.Process.Kill()
		}
	})

	plug := mtypes.Plugin{
		ID:     "test-plugin-clicommand",
		Path:   path,
		Stderr: stderr,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := clicommandv1.CapabilitySpec{
		SupportedCommands: []clicommandv1.CommandSpec{
			{Verb: "greet", ObjectType: "hello", Short: "Say hello"},
		},
	}
	require.NoError(t, registry.AddPlugin(plug, &capability))

	contract, err := registry.GetPlugin(ctx, "greet", "hello")
	require.NoError(t, err)
	require.NoError(t, registry.Shutdown(ctx))

	require.Eventually(t, func() bool {
		_, execErr := contract.Execute(ctx, &clicommandv1.ExecuteRequest{
			Verb: "greet", ObjectType: "hello",
		}, nil)
		if execErr != nil {
			if strings.Contains(execErr.Error(), "failed to send request to plugin") ||
				strings.Contains(execErr.Error(), "failed to execute CLI command via plugin") {
				return true
			}
			t.Logf("error: %v", execErr)
			return false
		}
		return false
	}, 1*time.Second, 100*time.Millisecond)
}

func TestRegisterInternalCLICommandPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewCLICommandRegistry(ctx)
	mock := &mockCLICommandPlugin{}

	r.NoError(registry.RegisterInternalCLICommandPlugin(
		clicommandv1.CommandSpec{Verb: "greet", ObjectType: "hello", Short: "Say hello"},
		mock,
	))

	// Listed commands should include the registered one.
	cmds := registry.ListCommands()
	r.Len(cmds, 1)
	r.Equal("greet", cmds[0].Verb)
	r.Equal("hello", cmds[0].ObjectType)

	// GetPlugin should return the mock.
	contract, err := registry.GetPlugin(ctx, "greet", "hello")
	r.NoError(err)
	r.NotNil(contract)

	// Execute should work.
	resp, err := contract.Execute(ctx, &clicommandv1.ExecuteRequest{
		Verb: "greet", ObjectType: "hello",
		Flags: map[string]string{"name": "world"},
	}, nil)
	r.NoError(err)
	r.Equal("hello, world!\n", resp.Output)
	r.True(mock.called)

	// Duplicate registration should error.
	r.Error(registry.RegisterInternalCLICommandPlugin(
		clicommandv1.CommandSpec{Verb: "greet", ObjectType: "hello"},
		mock,
	))

	// Unknown command should error.
	_, err = registry.GetPlugin(ctx, "unknown", "cmd")
	r.Error(err)
}

type mockCLICommandPlugin struct {
	called bool
}

var _ clicommandv1.CLICommandPluginContract = &mockCLICommandPlugin{}

func (m *mockCLICommandPlugin) Ping(_ context.Context) error { return nil }

func (m *mockCLICommandPlugin) GetCLICommandCredentialConsumerIdentity(
	_ context.Context,
	_ *clicommandv1.GetCredentialConsumerIdentityRequest,
) (*clicommandv1.GetCredentialConsumerIdentityResponse, error) {
	return &clicommandv1.GetCredentialConsumerIdentityResponse{}, nil
}

func (m *mockCLICommandPlugin) Execute(
	_ context.Context,
	req *clicommandv1.ExecuteRequest,
	_ map[string]string,
) (*clicommandv1.ExecuteResponse, error) {
	m.called = true
	name := "world"
	if v, ok := req.Flags["name"]; ok && v != "" {
		name = v
	}
	return &clicommandv1.ExecuteResponse{
		Output: "hello, " + name + "!\n",
	}, nil
}
