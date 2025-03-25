package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFlatMap(t *testing.T) {
	r := require.New(t)

	cfg, err := FlatMap(&Config{
		Type: runtime.NewUngroupedVersionedType(ConfigType, ConfigTypeV1),
		Configurations: []Configuration{
			{
				Raw: &runtime.Raw{
					Type: runtime.NewUngroupedVersionedType(ConfigType, ConfigTypeV1),
					Data: []byte(fmt.Sprintf(`{"type": "%[1]s", "configurations": [
{"type": "%[1]s", "configurations": [
	{"type": "custom-config", "key": "valuea"}
]}]}`, ConfigType+"/"+ConfigTypeV1)),
				},
			},
		},
	}, &Config{
		Type: runtime.NewUngroupedVersionedType(ConfigType, ConfigTypeV1),
		Configurations: []Configuration{
			{
				Raw: &runtime.Raw{
					Type: runtime.NewUngroupedVersionedType(ConfigType, ConfigTypeV1),
					Data: []byte(`{"key":"valuea","type":"custom-config"}`),
				},
			},
		},
	})
	r.NoError(err)
	r.Len(cfg.Configurations, 2)

	r.IsType(&runtime.Raw{}, cfg.Configurations[0].Raw)
	r.Equal(`{"key":"valuea","type":"custom-config"}`, string(cfg.Configurations[0].Raw.Data))
	r.IsType(&runtime.Raw{}, cfg.Configurations[1].Raw)
	r.Equal(`{"type": "generic.config.ocm.software/v1", "configurations": [
{"type": "generic.config.ocm.software/v1", "configurations": [
	{"type": "custom-config", "key": "valuea"}
]}]}`, string(cfg.Configurations[1].Raw.Data))
}
