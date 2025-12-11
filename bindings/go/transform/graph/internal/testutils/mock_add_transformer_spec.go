package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockAddObjectTransformerType = "MockAddObjectTransformer"

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

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockAddObjectTransformerOutput struct {
	Object MockObject `json:"object"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockAddObjectTransformerSpec struct {
	Object MockObject `json:"object"`
}
