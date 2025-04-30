package manager

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginManager(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	pm := NewPluginManager(ctx, logger)
	require.NoError(t, pm.RegisterPluginsAtLocation(ctx, "testdata"))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		require.NoError(t, os.Remove("/tmp/ocm_plugin_test-plugin.sock"))
	})

	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	desc, err := plugin.GetComponentVersion(ctx, mtypes.GetComponentVersionRequest[*v1.OCIRepository]{
		Repository: &v1.OCIRepository{
			Type:    typ,
			BaseUrl: "https://ocm.software/",
		},
		Name:    "test-component",
		Version: "1.0.0",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())
}
