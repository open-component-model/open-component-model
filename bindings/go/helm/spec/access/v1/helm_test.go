package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelm_ChartReference(t *testing.T) {
	tests := []struct {
		name     string
		helm     Helm
		expected string
	}{
		{
			name: "chart with version field",
			helm: Helm{
				HelmRepository: "https://charts.example.com",
				HelmChart:      "mariadb",
				Version:        "12.2.7",
			},
			expected: "https://charts.example.com/mariadb:12.2.7",
		},
		{
			name: "chart without version field",
			helm: Helm{
				HelmRepository: "https://charts.example.com",
				HelmChart:      "mariadb",
			},
			expected: "https://charts.example.com/mariadb",
		},
		{
			name: "chart with version in chart name",
			helm: Helm{
				HelmRepository: "https://charts.example.com",
				HelmChart:      "mariadb:12.2.7",
			},
			expected: "https://charts.example.com/mariadb:12.2.7",
		},
		{
			name: "oci registry style repository",
			helm: Helm{
				HelmRepository: "oci://ghcr.io/open-component-model/charts",
				HelmChart:      "my-chart",
				Version:        "1.0.0",
			},
			expected: "oci://ghcr.io/open-component-model/charts/my-chart:1.0.0",
		},
		{
			name: "empty version field is omitted",
			helm: Helm{
				HelmRepository: "https://charts.example.com",
				HelmChart:      "nginx",
				Version:        "",
			},
			expected: "https://charts.example.com/nginx",
		},
		{
			name: "trailing slash on repository is normalized",
			helm: Helm{
				HelmRepository: "oci://ghcr.io/charts/",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "oci://ghcr.io/charts/mychart:1.0.0",
		},
		{
			name: "multiple trailing slashes are normalized",
			helm: Helm{
				HelmRepository: "https://example.com///",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "https://example.com/mychart:1.0.0",
		},
		{
			name: "bare reference without scheme",
			helm: Helm{
				HelmRepository: "ghcr.io/charts",
				HelmChart:      "mychart",
				Version:        "1.0.0",
			},
			expected: "ghcr.io/charts/mychart:1.0.0",
		},
		{
			name: "version in chart name with colon",
			helm: Helm{
				HelmRepository: "oci://ghcr.io/charts",
				HelmChart:      "mychart:2.0.0",
				Version:        "",
			},
			expected: "oci://ghcr.io/charts/mychart:2.0.0",
		},
		{
			name: "relative path segments are resolved",
			helm: Helm{
				HelmRepository: "https://example.com/a/b",
				HelmChart:      "../escape",
				Version:        "1.0.0",
			},
			expected: "https://example.com/a/escape:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.helm.ChartReference()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHelm_GetChartName(t *testing.T) {
	tests := []struct {
		name     string
		chart    string
		expected string
	}{
		{
			name:     "plain chart name",
			chart:    "mariadb",
			expected: "mariadb",
		},
		{
			name:     "chart name with version",
			chart:    "mariadb:12.2.7",
			expected: "mariadb",
		},
		{
			name:     "empty chart name",
			chart:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Helm{HelmChart: tt.chart}
			assert.Equal(t, tt.expected, h.GetChartName())
		})
	}
}

func TestHelm_GetVersion(t *testing.T) {
	tests := []struct {
		name     string
		helm     Helm
		expected string
	}{
		{
			name:     "version from Version field",
			helm:     Helm{Version: "1.0.0", HelmChart: "mychart:2.0.0"},
			expected: "1.0.0",
		},
		{
			name:     "version from chart name",
			helm:     Helm{HelmChart: "mychart:2.0.0"},
			expected: "2.0.0",
		},
		{
			name:     "no version anywhere",
			helm:     Helm{HelmChart: "mychart"},
			expected: "",
		},
		{
			name:     "empty Version field falls back to chart name",
			helm:     Helm{Version: "", HelmChart: "mychart:3.0.0"},
			expected: "3.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.helm.GetVersion())
		})
	}
}
