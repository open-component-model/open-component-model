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

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

var dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.SigningHandlerPluginType)),
		SupportedSigningSpecTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
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
		PluginType: v1.SigningHandlerPluginType,
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
	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	retrievedPlugin, err := registry.GetPlugin(ctx, &runtime.Raw{Type: dummyType})
	require.NoError(t, err)

	// Call Sign via the signing.Handler abstraction and validate response
	sig, err := retrievedPlugin.Sign(ctx, descruntime.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "abc"}, &dummyv1.Repository{Type: dummyType, BaseUrl: "https://example"}, nil)
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
	config := mtypes.Config{ID: "test-plugin-signinghandler", Type: mtypes.Socket, PluginType: v1.SigningHandlerPluginType}
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

	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	retrievedPlugin, err := registry.GetPlugin(ctx, &runtime.Raw{Type: dummyType})
	require.NoError(t, err)
	require.NoError(t, registry.Shutdown(ctx))
	require.Eventually(t, func() bool {
		_, err = retrievedPlugin.Sign(ctx, descruntime.Digest{}, &dummyv1.Repository{Type: dummyType, BaseUrl: "https://example"}, nil)
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

func TestRegisterInternalComponentSignatureHandler(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewSigningRegistry(ctx)
	plugin := &mockSigningHandler{}
	r.NoError(registry.RegisterInternalComponentSignatureHandler(plugin))

	tests := []struct {
		name          string
		signingConfig runtime.Typed
		err           require.ErrorAssertionFunc
	}{
		{
			name:          "prototype",
			signingConfig: &dummyv1.Repository{},
			err:           require.NoError,
		},
		{
			name: "canonical type",
			signingConfig: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			signingConfig: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			signingConfig: &runtime.Raw{
				Type: runtime.Type{
					Name:    "NonExistingType",
					Version: "v1",
				},
			},
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resourceRepository, err := registry.GetPlugin(ctx, tc.signingConfig)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(resourceRepository)
		})
	}
}

type mockSigningHandler struct{ called bool }

var _ signing.Handler = &mockSigningHandler{}

func (m *mockSigningHandler) GetSigningHandlerScheme() *runtime.Scheme {
	return dummytype.Scheme
}

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
