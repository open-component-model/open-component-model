package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetHelmChartType = "GetHelmChart"

// GetHelmChart is a transformer specification to get a Helm chart.
// It specifies the resource to get the chart from and the output path where the chart should be downloaded to.
// This transformer is designed to support the helm access with classic helm charts.
// For OCI registry access, the OCI registry access transformer should be used instead, which can also handle Helm charts stored in OCI registries.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetHelmChart struct {
	// +ocm:jsonschema-gen:enum=GetHelmChart/v1alpha1
	Type   runtime.Type        `json:"type"`
	ID     string              `json:"id"`
	Spec   *GetHelmChartSpec   `json:"spec"`
	Output *GetHelmChartOutput `json:"output,omitempty"`
}

// GetHelmChartOutput is the output specification of the GetHelmChart transformation.
// It contains the file access specifications for the downloaded helm chart and prov file (if it exists),
// as well as the resource descriptor.
// The output path of the downloaded chart and prov files can be controlled by specifying the OutputPath in the GetHelmChartSpec.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetHelmChartOutput struct {
	// ChartFile is the file access specification for the downloaded helm chart
	ChartFile v1alpha1.File `json:"chartFile"`
	// ProvFile is the file access specification for the downloaded prov file, if it exists
	ProvFile *v1alpha1.File `json:"provFile,omitempty"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// GetHelmChartSpec is the input specification for the GetHelmChart transformation.
// Optionally, you can specify the output path where the chart should be downloaded to.
// If not specified, a temporary directory will be created where the chart and prov files will be downloaded to.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetHelmChartSpec struct {
	// Resource is the resource descriptor to get the artifact from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the path where the artifact should be downloaded to.
	// If empty, a temporary dir will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
