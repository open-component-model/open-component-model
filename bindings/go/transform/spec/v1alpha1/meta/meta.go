package meta

import "ocm.software/open-component-model/bindings/go/runtime"

// TransformationMeta contains metadata for a transformation.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type TransformationMeta struct {
	Type runtime.Type `json:"type"`
	ID   string       `json:"id"`
}
