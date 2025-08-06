package spec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFlatMap(t *testing.T) {
	r := require.New(t)

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
	r.Len(cfg.Configurations, 2)

	r.IsType(&runtime.Raw{}, cfg.Configurations[1])
	r.Equal(`{"key":"valuea","type":"custom-config-1"}`, string(cfg.Configurations[1].Data))
	r.IsType(&runtime.Raw{}, cfg.Configurations[0])
	r.Equal(`{"key":"valueb","type":"custom-config-2"}`, string(cfg.Configurations[0].Data))
}
