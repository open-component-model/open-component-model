package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConvertHelmToOCIType = "ConvertHelmToOCI"

// ConvertHelmToOCI is a transformer specification to convert a helm chart to an OCI artifact.
// It contains the resource descriptor of the helm chart to be converted and an optional output path.
// If no output path is given, a temporary file will be created for buffering the OCI artifact.
// Spec: ConvertHelmToOCISpec - the input specification of the transformation containing the resource descriptor and output path.
// Output: ConvertHelmToOCIOutput - the output specification of the transformation containing the file access specification
// for the converted OCI artifact and the resource descriptor from the component.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ConvertHelmToOCI struct {
	// +ocm:jsonschema-gen:enum=ConvertHelmToOCI/v1alpha1
	Type   runtime.Type            `json:"type"`
	ID     string                  `json:"id"`
	Spec   *ConvertHelmToOCISpec   `json:"spec"`
	Output *ConvertHelmToOCIOutput `json:"output,omitempty"`
}

// ConvertHelmToOCIOutput is the output specification of the ConvertHelmToOCI transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ConvertHelmToOCIOutput struct {
	// File is the file access specification for the converted OCI artifact
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// ConvertHelmToOCISpec is the input specification for the ConvertHelmToOCI transformation.
// It contains the resource descriptor of the helm chart to be converted and an optional output path.
// If no output path is given, a temporary file will be created for buffering the OCI artifact.
// ChartFile is the file access specification for the downloaded helm chart, which can be obtained from the output of the GetHelmChart transformation.
// ProvFile is the file access specification for the downloaded prov file.
// If provided, the OCI artifact will be created with the prov layer with media-type "application/vnd.cncf.helm.chart.provenance.v1.prov"
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ConvertHelmToOCISpec struct {
	// ChartFile is the file access specification for the downloaded helm chart
	ChartFile v1alpha1.File `json:"chartFile"`
	// ProvFile is the file access specification for the downloaded prov file, if it exists
	// +optional
	// +nullable
	ProvFile *v1alpha1.File `json:"provFile,omitempty"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the optional output the OCI artifact should be stored at.
	// If not specified, a temporary directory will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
