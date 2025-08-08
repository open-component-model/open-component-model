package transformers

import (
	extractspecv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
)

const (
	HELMTransformer = "helm"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"

	// LegacyChartLayerMediaType is the legacy reserved media type for Helm chart package content.
	LegacyChartLayerMediaType = "application/tar+gzip"
)

var HELMTransformerConfig = extractspecv1alpha1.Config{
	Rules: []extractspecv1alpha1.Rule{
		{
			Filename: "chart.tgz",
			LayerSelectors: []*extractspecv1alpha1.LayerSelector{
				{
					MatchExpressions: []extractspecv1alpha1.LayerSelectorRequirement{
						{
							Key:      extractspecv1alpha1.LayerMediaTypeKey,
							Operator: extractspecv1alpha1.LayerSelectorOpIn,
							Values: []string{
								ChartLayerMediaType,
								LegacyChartLayerMediaType,
							},
						},
					},
				},
			},
		},
		{
			Filename: "chart.tgz.prov",
			LayerSelectors: []*extractspecv1alpha1.LayerSelector{
				{
					MatchExpressions: []extractspecv1alpha1.LayerSelectorRequirement{
						{
							Key:      extractspecv1alpha1.LayerMediaTypeKey,
							Operator: extractspecv1alpha1.LayerSelectorOpIn,
							Values: []string{
								ProvLayerMediaType,
							},
						},
					},
				},
			},
		},
	},
}

func init() {
	DefaultTransformers[HELMTransformer] = &HELMTransformerConfig
}
