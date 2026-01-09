package test

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SampleType is a sample struct that includes a field of type runtime.Type.
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:example=examples/example-sample-type.yaml
type SampleType struct {
	Type runtime.Type `json:"type"`
	// Comment
	MyString string  `json:"myString"`
	MyInt    int     `json:"myInt"`
	MyBool   bool    `json:"myBool"`
	MyFloat  float64 `json:"myFloat"`
	// MyStructComment
	MyStruct struct {
		// MyComment
		MyString string `json:"myString"`
	}
	// MySliceComment
	MySlice []string `json:"mySlice"`
	// MyMapComment
	MyMap map[string]string `json:"myMap"`

	// MyUnknownMapComment
	MyUnknownMap map[string]any `json:"myUnknownMap"`

	// MyRuntimeComment
	MyRuntime runtime.Typed `json:"myRuntime"`
	// MyRawComment
	MyRaw *runtime.Raw `json:"myRaw"`

	// NestedComment
	Nested NestedType `json:"nested"`

	// NestedPointerComment
	NestedPointer *NestedType `json:"nestedPointer"`
}

// NestedType is a nested struct used for testing.
// +ocm:jsonschema-gen=true
type NestedType struct {
	// NestedFieldComment
	NestedField string `json:"nestedField"`
}

// MonoType is a struct with a single runtime.Typed field.
// +ocm:jsonschema-gen=true
type MonoType struct {
	// MyRuntimeComment
	MyCustomRuntime runtime.Typed `json:"myRuntime"`
}
