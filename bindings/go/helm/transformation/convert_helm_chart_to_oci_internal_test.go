package transformation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
)

func TestReferenceFromHelmAccess(t *testing.T) {
	tests := []struct {
		name        string
		helmAccess  v1.Helm
		expected    string
		expectError bool
		errContains string
	}{
		{
			name: "OCI repo with chart name and version field",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "charts/mychart:1.0.0",
		},
		{
			name: "OCI repo with version in chart name (colon-separated)",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "mychart:2.0.0",
			},
			expected: "charts/mychart:2.0.0",
		},
		{
			name: "HTTPS repo with chart name and version",
			helmAccess: v1.Helm{
				HelmRepository: "https://example.com",
				HelmChart:      "mychart",
				Version:        "0.1.0",
			},
			expected: "mychart:0.1.0",
		},
		{
			name: "chart name without version produces reference without tag",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "mychart",
			},
			expected: "charts/mychart",
		},
		{
			name: "version field takes precedence over version in chart name",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "mychart:1.0.0",
				Version:        "2.0.0",
			},
			expected: "charts/mychart:2.0.0",
		},
		{
			name: "nested repository path",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/org/charts",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "org/charts/mychart:1.0.0",
		},
		{
			name: "empty HelmRepository still produces reference from chart name",
			helmAccess: v1.Helm{
				HelmRepository: "",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "mychart:1.0.0",
		},
		{
			name: "empty HelmChart returns error",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "",
				Version:        "1.0.0",
			},
			expectError: true,
			errContains: "chart name is required",
		},
		{
			name: "both empty returns error",
			helmAccess: v1.Helm{
				HelmRepository: "",
				HelmChart:      "",
			},
			expectError: true,
			errContains: "chart name is required",
		},
		{
			name: "trailing slash on repository is normalized",
			helmAccess: v1.Helm{
				HelmRepository: "oci://ghcr.io/charts/",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "charts/mychart:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := imageReferenceFromHelmAccess(tt.helmAccess)
			if tt.expectError {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, ref)
			}
		})
	}
}
