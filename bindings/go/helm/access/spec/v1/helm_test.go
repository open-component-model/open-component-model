package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.helm.ChartReference()
			assert.Equal(t, tt.expected, result)
		})
	}
}
