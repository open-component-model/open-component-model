package manager

import (
	"context"
	"github.com/stretchr/testify/require"
	"log/slog"
	"ocm.software/open-component-model/bindings/go/runtime"
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
	t.Cleanup(func() {
		r.NoError(pm.Shutdown(testctx))
	})

	got, err := GetTransferPlugin[*RepositoryPlugin](testctx, pm, "ReadWriteComponentVersionRepository", &mockType{})
	r.NoError(err)
	r.NotNil(got)
}
