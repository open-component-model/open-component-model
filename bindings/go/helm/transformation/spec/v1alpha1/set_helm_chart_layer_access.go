package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const SetHelmChartLayerAccessType = "SetHelmChartLayerAccess"

// SetHelmChartLayerAccess is a transformer specification that points a Helm chart resource
// at the chart layer of the Helm OCI image the chart was pushed as.
// A copied chart keeps the digest its source records, and that digest names the chart archive,
// not the image manifest. So the access must address the chart layer (an ociBlob/v1 access),
// or the recorded digest would no longer match the content the access returns.
// Spec: SetHelmChartLayerAccessSpec - the resource, the image reference, and the chart layer.
// Output: SetHelmChartLayerAccessOutput - the resource with its access replaced.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SetHelmChartLayerAccess struct {
	// +ocm:jsonschema-gen:enum=SetHelmChartLayerAccess/v1alpha1
	Type   runtime.Type                   `json:"type"`
	ID     string                         `json:"id"`
	Spec   *SetHelmChartLayerAccessSpec   `json:"spec"`
	Output *SetHelmChartLayerAccessOutput `json:"output,omitzero"`
}

// SetHelmChartLayerAccessSpec is the input specification of the SetHelmChartLayerAccess transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type SetHelmChartLayerAccessSpec struct {
	// Resource is the Helm chart resource as the source component version records it.
	Resource *v2.Resource `json:"resource"`
	// ImageReference is the reference of the Helm OCI image the chart was pushed as.
	// Its tag and digest are dropped, because a layer is addressed by its own digest.
	ImageReference string `json:"imageReference"`
	// ChartLayer describes the chart layer of that image.
	ChartLayer ChartLayer `json:"chartLayer"`
}

// SetHelmChartLayerAccessOutput is the output specification of the SetHelmChartLayerAccess transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type SetHelmChartLayerAccessOutput struct {
	// Resource is the input resource with its access replaced by an access to the chart layer.
	// Its digest is left as recorded.
	Resource *v2.Resource `json:"resource"`
}
