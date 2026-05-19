package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/helm/internal"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		repository       string
		expectedType     runtime.Type
		expectedIdentity map[string]string
		expectedErr      string
	}{
		{
			name:         "returns HelmChartRepository identity for HTTPS repository",
			repository:   "https://charts.example.com/stable",
			expectedType: runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType),
			expectedIdentity: map[string]string{
				"scheme":   "https",
				"hostname": "charts.example.com",
			},
		},
		{
			name:         "returns HelmChartRepository identity for HTTP repository",
			repository:   "http://charts.example.com:8080/repo",
			expectedType: runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType),
			expectedIdentity: map[string]string{
				"scheme":   "http",
				"hostname": "charts.example.com",
				"port":     "8080",
			},
		},
		{
			name:         "returns OCIRegistry identity for OCI repository",
			repository:   "oci://registry.example.com/charts/mychart:1.0.0",
			expectedType: ocicredentialsspecv1.Type,
			expectedIdentity: map[string]string{
				"scheme":   "oci",
				"hostname": "registry.example.com",
			},
		},
		{
			name:        "returns error for empty repository (local helm input)",
			repository:  "",
			expectedErr: "no helm repository specified",
		},
		{
			name:        "returns error for invalid URL",
			repository:  "://invalid",
			expectedErr: "error parsing helm repository URL to identity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity, err := internal.CredentialConsumerIdentity(tt.repository)

			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				assert.Nil(t, identity)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, identity)
			assert.Equal(t, tt.expectedType, identity.GetType())
			for key, value := range tt.expectedIdentity {
				assert.Equal(t, value, identity[key])
			}
		})
	}
}
