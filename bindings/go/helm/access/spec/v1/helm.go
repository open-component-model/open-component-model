package v1

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	LegacyType        = "helm"
	LegacyTypeVersion = "v1"
)

// Helm describes the access for a helm chart.
// This spec is aligned with ocm v1 https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/accessmethods/helm/method.go#L41
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Helm struct {
	// +ocm:jsonschema-gen:enum=Helm/v1,helm/v1
	// +ocm:jsonschema-gen:enum:deprecated=Helm,helm
	Type runtime.Type `json:"type"`

	// HelmRepository is the URL of the helm repository to load the chart from.
	HelmRepository string `json:"helmRepository"`

	// HelmChart is the name of the helm chart and its version separated by a colon.
	HelmChart string `json:"helmChart"`

	// Version can either be specified as part of the chart name or separately.
	Version string `json:"version,omitempty"`
}

func (h *Helm) ChartReference() string {
	parts := []string{
		h.HelmRepository,
		h.GetChartName(),
	}

	chart := strings.Join(parts, "/")

	version := h.GetVersion()
	if version != "" {
		chart = chart + ":" + version
	}

	return chart
}

func (h *Helm) GetChartName() string {
	chartParts := strings.Split(h.HelmChart, ":")
	return chartParts[0]
}

func (h *Helm) GetVersion() string {
	if h.Version != "" {
		return h.Version
	}

	// If version is not specified separately, try to parse it from the chart name.
	chartParts := strings.Split(h.HelmChart, ":")
	if len(chartParts) == 2 {
		return chartParts[1]
	}

	return ""
}
