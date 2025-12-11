package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockAddObjectTransformerType = "MockAddObjectTransformer"

// MockAddObjectTransformer is a transformer that mocks adding an object.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MockAddObjectTransformer struct {
	// +ocm:jsonschema-gen:enum=MockAddObjectTransformer/v1alpha1
	Type   runtime.Type                    `json:"type"`
	ID     string                          `json:"id,omitempty"`
	Spec   *MockAddObjectTransformerSpec   `json:"spec"`
	Output *MockAddObjectTransformerOutput `json:"output,omitempty"`
}

// MockAddObjectTransformerOutput is the output of the MockAddObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockAddObjectTransformerOutput struct {
	Object MockObject `json:"object"`
}

// MockAddObjectTransformerSpec is the spec of the MockAddObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockAddObjectTransformerSpec struct {
	Object MockObject `json:"object"`
}
