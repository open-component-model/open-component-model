package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockGetObjectTransformerType = "MockGetObjectTransformerType"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformer struct {
	// +ocm:jsonschema-gen:enum=CTFGetComponentVersion/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id,omitempty"`
	Spec   *MockGetObjectTransformerSpec   `json:"spec"`
	Output *MockGetObjectTransformerOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformerOutput struct {
	Object MockObject `json:"descriptor"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformerSpec struct {
	Name    string
	Version string
}

type MockObject struct {
	Name    string
	Version string
	Content string
}
