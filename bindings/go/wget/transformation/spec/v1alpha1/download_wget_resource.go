package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const DownloadWgetResourceType = "DownloadWgetResource"

// DownloadWgetResource is a transformer specification to download a wget resource to a file.
// It specifies the resource to download and the output path where the content should be written to.
// The downloaded content is a plain blob; it is intended to be embedded as a local blob in the
// target repository by a subsequent AddLocalResource transformation.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DownloadWgetResource struct {
	// +ocm:jsonschema-gen:enum=DownloadWgetResource/v1alpha1
	Type   runtime.Type   `json:"type"`
	ID     string         `json:"id"`
	Spec   *DownloadWgetResourceSpec   `json:"spec"`
	Output *DownloadWgetResourceOutput `json:"output,omitempty"`
}

// DownloadWgetResourceOutput is the output specification of the DownloadWgetResource transformation.
// It contains the file access specification for the downloaded content, as well as the resource descriptor.
// The output path of the downloaded content can be controlled by specifying the OutputPath in the DownloadWgetResourceSpec.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadWgetResourceOutput struct {
	// File is the file access specification for the downloaded content.
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component.
	Resource *v2.Resource `json:"resource"`
}

// DownloadWgetResourceSpec is the input specification for the DownloadWgetResource transformation.
// Optionally, you can specify the output path where the content should be downloaded to.
// If not specified, a temporary file will be created where the content will be downloaded to.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadWgetResourceSpec struct {
	// Resource is the resource descriptor to get the content from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the path where the content should be downloaded to.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
