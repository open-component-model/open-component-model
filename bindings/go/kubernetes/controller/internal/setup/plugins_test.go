package setup_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
)

func cachingConfig(t *testing.T, body string) *genericv1.Config {
	t.Helper()
	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: caching.oci.config.ocm.software/v1alpha1
`+body), &cfg))
	return &cfg
}

func TestNewPluginManager_OCICachingConfig(t *testing.T) {
	for name, body := range map[string]string{
		"invalid mode":    "    mode: Sometimes\n",
		"invalid ttl":     "    ttl: -1m\n",
		"invalid maxSize": "    maxBlobSize: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			pm, err := setup.NewPluginManager(t.Context(), cachingConfig(t, body), slog.Default())
			require.Error(t, err)
			require.Nil(t, pm)
			require.Contains(t, err.Error(), "OCI caching")
		})
	}

	for _, body := range []string{"    mode: Never\n", "    mode: IfNotPresent\n    ttl: 1m\n    maxBlobSize: 1024\n"} {
		pm, err := setup.NewPluginManager(t.Context(), cachingConfig(t, body), slog.Default())
		require.NoError(t, err)
		require.NotNil(t, pm)
	}

	pm, err := setup.NewPluginManager(t.Context(), nil, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, pm)
}
