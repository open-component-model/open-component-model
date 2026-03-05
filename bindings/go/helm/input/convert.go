package input

import (
	"ocm.software/open-component-model/bindings/go/helm"
)

func ConvertFromReadOnlyChart(chart *helm.ChartData) *ReadOnlyChart {
	if chart == nil {
		return nil
	}
	return &ReadOnlyChart{
		Name:         chart.Name,
		Version:      chart.Version,
		ChartBlob:    chart.ChartBlob,
		ProvBlob:     chart.ProvBlob,
		chartTempDir: chart.ChartTempDir,
	}
}

func ConvertToReadOnlyChart(chart *ReadOnlyChart) *helm.ChartData {
	if chart == nil {
		return nil
	}
	return &helm.ChartData{
		Name:         chart.Name,
		Version:      chart.Version,
		ChartBlob:    chart.ChartBlob,
		ProvBlob:     chart.ProvBlob,
		ChartTempDir: chart.chartTempDir,
	}
}
