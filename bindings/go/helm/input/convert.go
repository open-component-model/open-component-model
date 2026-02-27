package input

import (
	"ocm.software/open-component-model/bindings/go/helm"
)

func ConvertFromReadOnlyChart(chart *helm.ReadOnlyChart) *ReadOnlyChart {
	return &ReadOnlyChart{
		Name:         chart.Name,
		Version:      chart.Version,
		ChartBlob:    chart.ChartBlob,
		ProvBlob:     chart.ProvBlob,
		chartTempDir: chart.ChartTempDir,
	}
}

func ConvertToReadOnlyChart(chart *ReadOnlyChart) *helm.ReadOnlyChart {
	return &helm.ReadOnlyChart{
		Name:         chart.Name,
		Version:      chart.Version,
		ChartBlob:    chart.ChartBlob,
		ProvBlob:     chart.ProvBlob,
		ChartTempDir: chart.chartTempDir,
	}
}
