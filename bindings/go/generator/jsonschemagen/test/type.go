package test

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SampleType is a sample struct that includes a field of type runtime.Type.
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SampleType struct {
	Type     runtime.Type `json:"type"`
	MyString string       `json:"myString"`
	MyInt    int          `json:"myInt"`
	MyBool   bool         `json:"myBool"`
	MyFloat  float64      `json:"myFloat"`
	MyStruct struct {
		MyString string `json:"myString"`
	}
	MySlice []string          `json:"mySlice"`
	MyMap   map[string]string `json:"myMap"`

	MyRuntime runtime.Typed `json:"myRuntime"`
	MyRaw     *runtime.Raw  `json:"myRaw"`
}
