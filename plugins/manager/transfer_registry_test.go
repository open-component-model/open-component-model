package manager

import (
	"context"
	"github.com/stretchr/testify/require"
	"log/slog"
	"ocm.software/open-component-model/bindings/go/runtime"
	"os"
	"testing"
	"time"
)

type mockType struct {
	typ runtime.Type
}

func (m *mockType) GetType() runtime.Type {
	return runtime.Type{
		Version: "v1",
		Name:    "OCIRegistry",
	}
}

func (m *mockType) SetType(t runtime.Type) {
	m.typ = t
}

func (m *mockType) DeepCopyTyped() runtime.Typed {
	return m
}

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

	got, err := GetReadWriteComponentVersionRepository(testctx, pm, &mockType{})
	r.NoError(err)
	r.NoError(got.GetLocalResource(testctx, GetLocalResourceRequest{
		TargetLocation: Location{
			Value: tmp.Name(),
		},
	}, nil))
}
