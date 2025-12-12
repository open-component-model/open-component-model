package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockGetObjectTransformerType = "MockGetObjectTransformer"

// MockGetObjectTransformer is a transformer that mocks getting an object.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformer struct {
	// +ocm:jsonschema-gen:enum=MockGetObjectTransformer/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id,omitempty"`
	Spec   *MockGetObjectTransformerSpec   `json:"spec"`
	Output *MockGetObjectTransformerOutput `json:"output,omitempty"`
}

// MockGetObjectTransformerOutput is the output of the MockGetObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformerOutput struct {
	Object MockObject `json:"object"`
}

// MockGetObjectTransformerSpec is the spec of the MockGetObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockGetObjectTransformerSpec struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
