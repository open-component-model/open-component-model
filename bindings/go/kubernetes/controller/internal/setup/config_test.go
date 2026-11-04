package setup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestLoadStartupConfig(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantPolicy ocicachev1alpha1.RemotePolicy
		wantErr    string
	}{
		{
			name: "oci cache config",
			content: `type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
`,
			wantPolicy: ocicachev1alpha1.RemotePolicyAlways,
		},
		{
			name: "unversioned oci cache config",
			content: `type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software
    remotePolicy: IfNotPresent
`,
			wantPolicy: ocicachev1alpha1.RemotePolicyIfNotPresent,
		},
		{
			name: "unsupported type",
			content: `type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
  - type: http.config.ocm.software/v1alpha1
    timeout: 45s
  - type: credentials.config.ocm.software
`,
			wantErr: "types [http.config.ocm.software/v1alpha1 credentials.config.ocm.software] are not supported at startup",
		},
		{
			name:    "malformed",
			content: "configurations: [",
			wantErr: "failed to decode ocm config",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			r.NoError(os.WriteFile(path, []byte(tc.content), 0o600))

			cfg, err := setup.LoadStartupConfig(path)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			cacheCfg, err := ocicachev1alpha1.LookupConfig(cfg)
			r.NoError(err)
			r.Equal(tc.wantPolicy, cacheCfg.RemotePolicy)
		})
	}

	t.Run("missing file", func(t *testing.T) {
		_, err := setup.LoadStartupConfig(filepath.Join(t.TempDir(), "missing.yaml"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
