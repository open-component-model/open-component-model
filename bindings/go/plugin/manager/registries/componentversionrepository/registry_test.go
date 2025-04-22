package componentversionrepository

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	goruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetTransferPlugin(t *testing.T) {
	r := require.New(t)
	testctx := context.Background()
	pm := manager.NewPluginManager(testctx, slog.New(slog.DiscardHandler))
	location := "testdata/darwin"
	if runtime.GOOS == "linux" {
		location = "testdata/linux"
	}
	err := pm.RegisterPluginsAtLocation(testctx, location, manager.WithIdleTimeout(10*time.Second))
	r.NoError(err)
	tmp, err := os.CreateTemp("", "test.file")
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(os.RemoveAll("/tmp/ocm_plugin_generic.sock"))
		r.NoError(pm.Shutdown(testctx))
		r.NoError(tmp.Close())
		r.NoError(os.Remove(tmp.Name()))
	})

	got, err := GetReadWriteComponentVersionRepositoryPluginForType(testctx, pm.ComponentVersionRepositoryRegistry, &v2.OCIRepository{})
	r.NoError(err)
	r.NoError(got.GetLocalResource(testctx, types.GetLocalResourceRequest[*v2.OCIRepository]{
		Repository: &v2.OCIRepository{
			Type: goruntime.Type{
				Version: "OCIRepository",
				Name:    "v2",
			},
			BaseUrl: "ghcr.io/open-component-model/ocm",
		},
		Name:    "name",
		Version: "v1",
		TargetLocation: types.Location{
			Value: tmp.Name(),
		},
	}, nil))
}
