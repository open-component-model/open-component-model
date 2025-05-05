package manager

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginManager(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp")))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		require.NoError(t, os.Remove("/tmp/test-plugin-plugin.socket"))
	})

	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	desc, err := plugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[*v1.OCIRepository]{
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
