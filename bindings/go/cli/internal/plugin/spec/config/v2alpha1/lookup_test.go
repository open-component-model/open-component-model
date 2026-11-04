package v2alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupConfig_AcceptsVersionedAndUnversionedType(t *testing.T) {
	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	} {
		t.Run(typ.String(), func(t *testing.T) {
			r := require.New(t)
			raw := &runtime.Raw{}
			r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&Config{
				Type:        typ,
				IdleTimeout: Duration(7 * time.Second),
				Locations:   []string{"/plugins"},
			}, raw))

			cfg, err := LookupConfig(&generic.Config{
				Type:           runtime.NewVersionedType(generic.ConfigType, generic.ConfigTypeV1),
				Configurations: []*runtime.Raw{raw},
			})
			r.NoError(err)
			r.Equal(Duration(7*time.Second), cfg.IdleTimeout)
			r.Equal([]string{"/plugins"}, cfg.Locations)
		})
	}
}
