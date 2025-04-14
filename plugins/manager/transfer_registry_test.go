package manager

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

func TestGetTransferPlugin(t *testing.T) {
	r := require.New(t)
	testctx := context.Background()
	pm := NewPluginManager(testctx, slog.New(slog.DiscardHandler))
	err := pm.RegisterPluginsAtLocation(testctx, "testdata", WithIdleTimeout(10*time.Second))
	r.NoError(err)
	tmp, err := os.CreateTemp("", "test.file")
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(pm.Shutdown(testctx))
		r.NoError(tmp.Close())
		r.NoError(os.Remove(tmp.Name()))
	})

	got, err := GetReadWriteComponentVersionRepository(testctx, pm, &v1.OCIImage{})
	r.NoError(err)
	r.NoError(got.GetLocalResource(testctx, GetLocalResourceRequest{
		TargetLocation: Location{
			Value: tmp.Name(),
		},
	}, nil))
}
