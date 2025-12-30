package input

import (
	"context"
	"testing"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWasmInputPlugin(t *testing.T) {
	ctx := context.Background()
	wasmPath := "../../../tmp/testdata/test-plugin-wasm-input.wasm"

	t.Run("create wasm plugin", func(t *testing.T) {
		plugin, err := NewWasmInputPlugin(ctx, wasmPath, "test-wasm-plugin")
		require.NoError(t, err)
		require.NotNil(t, plugin)
		defer plugin.Close()

		// Test Ping
		err = plugin.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("process resource", func(t *testing.T) {
		plugin, err := NewWasmInputPlugin(ctx, wasmPath, "test-wasm-plugin")
		require.NoError(t, err)
		defer plugin.Close()

		request := &v1.ProcessResourceInputRequest{
			Resource: &constructorv1.Resource{
				ElementMeta: constructorv1.ElementMeta{
					ObjectMeta: constructorv1.ObjectMeta{
						Name:    "test-resource",
						Version: "v1.0.0",
					},
				},
				Type: "wasmTestInput",
			},
		}

		response, err := plugin.ProcessResource(ctx, request, map[string]string{})
		require.NoError(t, err)
		require.NotNil(t, response)

		assert.Equal(t, "test-resource", response.Resource.Name)
		assert.NotNil(t, response.Location)
	})

	t.Run("process source", func(t *testing.T) {
		plugin, err := NewWasmInputPlugin(ctx, wasmPath, "test-wasm-plugin")
		require.NoError(t, err)
		defer plugin.Close()

		request := &v1.ProcessSourceInputRequest{
			Source: &constructorv1.Source{
				ElementMeta: constructorv1.ElementMeta{
					ObjectMeta: constructorv1.ObjectMeta{
						Name:    "test-source",
						Version: "v1.0.0",
					},
				},
				Type: "wasmTestInput",
			},
		}

		response, err := plugin.ProcessSource(ctx, request, map[string]string{})
		require.NoError(t, err)
		require.NotNil(t, response)

		assert.Equal(t, "test-source", response.Source.Name)
		assert.NotNil(t, response.Location)
	})
}
