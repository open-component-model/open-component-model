package replication

import (
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
)

// The transfer gets the HTTP configuration of the replication, so a host that
// enables plainHTTP there is reached over plain HTTP by the transfer as well.
func TestTransferOptions(t *testing.T) {
	plainHost := &runtime.Raw{
		Type: runtime.NewVersionedType(httpv1alpha1.ConfigType, httpv1alpha1.Version),
		Data: []byte(`{"type":"` + httpv1alpha1.ConfigType + `/` + httpv1alpha1.Version + `","hosts":{"registry.local:5000":{"plainHTTP":true}}}`),
	}
	for _, tc := range []struct {
		name      string
		cfg       *configuration.Configuration
		wantPlain bool
	}{
		{name: "no configuration"},
		{name: "plainHTTP for a host", cfg: &configuration.Configuration{Config: &genericv1.Config{
			Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
			Configurations: []*runtime.Raw{plainHost},
		}}, wantPlain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			opts, err := transferOptions(tc.cfg)
			r.NoError(err)
			var o transfer.Options
			for _, opt := range opts {
				opt(&o)
			}
			r.NotNil(o.HTTPConfig, "the transfer must always get an HTTP configuration")
			plain := o.HTTPConfig.ResolveHost("registry.local:5000").PlainHTTP
			r.Equal(tc.wantPlain, plain != nil && *plain)
		})
	}
}
