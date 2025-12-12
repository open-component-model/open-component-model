package test

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SampleType is a sample struct that includes a field of type runtime.Type.
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SampleType struct {
	Type runtime.Type `json:"type"`
}
