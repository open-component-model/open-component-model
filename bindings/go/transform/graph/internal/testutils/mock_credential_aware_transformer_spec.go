package testutils

import "ocm.software/open-component-model/bindings/go/runtime"

const MockCredentialAwareTransformerType = "MockCredentialAwareTransformer"

// MockCredentialAwareTransformer is a transformer that mocks credential-aware transformation.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MockCredentialAwareTransformer struct {
	// +ocm:jsonschema-gen:enum=MockCredentialAwareTransformer/v1alpha1
	Type   runtime.Type                          `json:"type"`
	ID     string                                `json:"id,omitempty"`
	Spec   *MockCredentialAwareTransformerSpec   `json:"spec"`
	Output *MockCredentialAwareTransformerOutput `json:"output,omitempty"`
}

// MockCredentialAwareTransformerOutput is the output of the MockCredentialAwareTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockCredentialAwareTransformerOutput struct {
	Object MockObject `json:"object"`
}

// MockCredentialAwareTransformerSpec is the spec of the MockCredentialAwareTransformer.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MockCredentialAwareTransformerSpec struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
