package test

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type SampleType struct {
	Type runtime.Type `json:"type"`
}
