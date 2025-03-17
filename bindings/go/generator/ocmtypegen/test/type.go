package test

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// +k8s:deepcopy-gen=true
//
//go:generate ocmtypegen $GOFILE
type SampleType struct {
	Type runtime.Type `json:"type"`
}
