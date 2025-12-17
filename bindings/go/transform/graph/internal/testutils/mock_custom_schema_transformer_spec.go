package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockCustomSchemaObjectTransformerType = "MockCustomSchemaObjectTransformer"

// MockCustomSchemaObjectTransformer is a transformer that mocks adding an object.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MockCustomSchemaObjectTransformer struct {
	// +ocm:jsonschema-gen:enum=MockCustomSchemaObjectTransformer/v1alpha1
	Type   runtime.Type                             `json:"type"`
	ID     string                                   `json:"id,omitempty"`
	Spec   *MockCustomSchemaObjectTransformerSpec   `json:"spec"`
	Output *MockCustomSchemaObjectTransformerOutput `json:"output,omitempty"`
}

// MockCustomSchemaObjectTransformerOutput is the output of the MockCustomSchemaObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockCustomSchemaObjectTransformerOutput struct {
	String string `json:"content"`
}

// MockCustomSchemaObjectTransformerSpec is the spec of the MockCustomSchemaObjectTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockCustomSchemaObjectTransformerSpec struct {
	Object *MockCustomSchemaObject `json:"object"`
}
