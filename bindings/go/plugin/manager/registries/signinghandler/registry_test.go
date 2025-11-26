package signinghandler

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
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

var DummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)

type mockSigningHandler struct{ called bool }

func (m *mockSigningHandler) GetSigningCredentialConsumerIdentity(ctx context.Context, name string, unsigned descruntime.Digest, config runtime.Typed) (runtime.Identity, error) {
	m.called = true
	return runtime.Identity{"id": "x"}, nil
}

func (m *mockSigningHandler) Sign(ctx context.Context, unsigned descruntime.Digest, config runtime.Typed, credentials map[string]string) (descruntime.SignatureInfo, error) {
	m.called = true
	return descruntime.SignatureInfo{}, nil
}

func (m *mockSigningHandler) GetVerifyingCredentialConsumerIdentity(ctx context.Context, signed descruntime.Signature, config runtime.Typed) (runtime.Identity, error) {
	m.called = true
	return runtime.Identity{"id": "y"}, nil
}

func (m *mockSigningHandler) Verify(ctx context.Context, signed descruntime.Signature, config runtime.Typed, credentials map[string]string) error {
	m.called = true
	return nil
}

var _ signing.Handler = &mockSigningHandler{}

func TestRegisterInternalComponentSignatureHandler(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewSigningRegistry(ctx)
	p := &mockSigningHandler{}
	require.NoError(t, RegisterInternalComponentSignatureHandler(scheme, registry, p, &dummyv1.Repository{}))
	retrievedPlugin, err := registry.GetPlugin(ctx, &dummyv1.Repository{})
	require.NoError(t, err)
	require.Equal(t, p, retrievedPlugin)
	_, err = retrievedPlugin.GetSigningCredentialConsumerIdentity(ctx, "name", descruntime.Digest{}, &runtime.Raw{Type: DummyType, Data: []byte(`{}`)})
	require.NoError(t, err)
	require.True(t, p.called)
}

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-signinghandler")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-signinghandler first")

	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewSigningRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-signinghandler",
		Type:       mtypes.Socket,
		PluginType: mtypes.SigningHandlerPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-signinghandler-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-signinghandler",
		Path:   path,
		Stderr: stderr,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.SigningHandlerPluginType)),
		TypeToJSONSchema: map[string][]byte{
			DummyType.String(): []byte(`{}`),
		},
		SupportedSigningSpecTypes: []mtypes.Type{
			{
				Type:    DummyType,
				Aliases: nil,
			},
		},
	}
	require.NoError(t, registry.AddPluginWithAliases(plugin, &capability))
	retrievedPlugin, err := registry.GetPlugin(ctx, &runtime.Raw{Type: DummyType})
	require.NoError(t, err)

	// Call Sign via the signing.Handler abstraction and validate response
	sig, err := retrievedPlugin.Sign(ctx, descruntime.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "abc"}, &dummyv1.Repository{Type: DummyType, BaseUrl: "https://example"}, nil)
	require.NoError(t, err)
	require.Equal(t, "rsa", sig.Algorithm)
	require.Equal(t, "sig", sig.Value)
}

func TestShutdown(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-signinghandler")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-signinghandler first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewSigningRegistry(ctx)
	config := mtypes.Config{ID: "test-plugin-signinghandler", Type: mtypes.Socket, PluginType: mtypes.SigningHandlerPluginType}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-signinghandler-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-signinghandler",
		Path:   path,
		Stderr: stderr,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.SigningHandlerPluginType)),
		TypeToJSONSchema: map[string][]byte{
			DummyType.String(): []byte(`{}`),
		},
		SupportedSigningSpecTypes: []mtypes.Type{
			{
				Type:    DummyType,
				Aliases: nil,
			},
		},
	}

	require.NoError(t, registry.AddPluginWithAliases(plugin, &capability))
	retrievedPlugin, err := registry.GetPlugin(ctx, &runtime.Raw{Type: DummyType})
	require.NoError(t, err)
	require.NoError(t, registry.Shutdown(ctx))
	require.Eventually(t, func() bool {
		_, err = retrievedPlugin.Sign(ctx, descruntime.Digest{}, &dummyv1.Repository{Type: DummyType, BaseUrl: "https://example"}, nil)
		if err != nil {
			if strings.Contains(err.Error(), "failed to send request to plugin") {
				return true
			}
			t.Logf("error: %v", err)
			return false
		}
		return false
	}, 1*time.Second, 100*time.Millisecond)
}
