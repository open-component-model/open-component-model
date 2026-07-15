package spec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFlatMap(t *testing.T) {
	t.Run("deeply nested generic is flattened in declaration order", func(t *testing.T) {
		cfg := FlatMap(&Config{
			Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			Configurations: []*runtime.Raw{
				{
					Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
					Data: []byte(fmt.Sprintf(`{"type": "%[1]s", "configurations": [
{"type": "%[1]s", "configurations": [
	{"type": "custom-config-1", "key": "valuea"}
]}]}`, ConfigType+"/"+ConfigTypeV1)),
				},
			},
		}, &Config{
			Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			Configurations: []*runtime.Raw{
				{
					Type: runtime.NewUnversionedType("custom-config-2"),
					Data: []byte(`{"key":"valueb","type":"custom-config-2"}`),
				},
			},
		})
		require.Len(t, cfg.Configurations, 2)

		assert.Equal(t, `{"key":"valuea","type":"custom-config-1"}`, string(cfg.Configurations[0].Data),
			"deeply nested entry from first input should appear first")
		assert.Equal(t, `{"key":"valueb","type":"custom-config-2"}`, string(cfg.Configurations[1].Data),
			"flat entry from second input should appear second")
	})

	t.Run("multi-file merge preserves input order", func(t *testing.T) {
		file1 := &Config{
			Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			Configurations: []*runtime.Raw{
				{Type: runtime.NewUnversionedType("typeA"), Data: []byte(`{"type":"typeA","v":"file1-a"}`)},
				{Type: runtime.NewUnversionedType("typeB"), Data: []byte(`{"type":"typeB","v":"file1-b"}`)},
			},
		}
		file2 := &Config{
			Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			Configurations: []*runtime.Raw{
				{Type: runtime.NewUnversionedType("typeA"), Data: []byte(`{"type":"typeA","v":"file2-a"}`)},
			},
		}

		cfg := FlatMap(file1, file2)
		require.Len(t, cfg.Configurations, 3)

		assert.Equal(t, `{"type":"typeA","v":"file1-a"}`, string(cfg.Configurations[0].Data),
			"first file's first entry should be at index 0")
		assert.Equal(t, `{"type":"typeB","v":"file1-b"}`, string(cfg.Configurations[1].Data),
			"first file's second entry should be at index 1")
		assert.Equal(t, `{"type":"typeA","v":"file2-a"}`, string(cfg.Configurations[2].Data),
			"second file's entry should be at index 2")
	})

	t.Run("nested generic entries appear after direct siblings", func(t *testing.T) {
		nestedJSON := fmt.Sprintf(
			`{"type":"%s","configurations":[{"type":"inner","v":"nested"}]}`,
			ConfigType+"/"+ConfigTypeV1)

		file := &Config{
			Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			Configurations: []*runtime.Raw{
				{Type: runtime.NewUnversionedType("before"), Data: []byte(`{"type":"before","v":"1"}`)},
				{Type: runtime.NewVersionedType(ConfigType, ConfigTypeV1), Data: []byte(nestedJSON)},
				{Type: runtime.NewUnversionedType("after"), Data: []byte(`{"type":"after","v":"3"}`)},
			},
		}

		cfg := FlatMap(file)
		require.Len(t, cfg.Configurations, 3)

		assert.Equal(t, `{"type":"before","v":"1"}`, string(cfg.Configurations[0].Data),
			"direct entry before nested generic keeps its position")
		assert.Equal(t, `{"type":"after","v":"3"}`, string(cfg.Configurations[1].Data),
			"direct entry after nested generic comes before nested children")
		assert.Equal(t, `{"type":"inner","v":"nested"}`, string(cfg.Configurations[2].Data),
			"nested entry appears after all direct siblings (last wins)")
	})
}
