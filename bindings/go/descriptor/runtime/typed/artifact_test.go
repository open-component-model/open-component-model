package typed_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	untyped "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime/typed"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockAccess implements runtime.Typed for testing and supports JSON marshalling.
type MockAccess struct {
	Type string `json:"type"`
}

func (m *MockAccess) GetType() runtime.Type {
	return runtime.NewUnversionedType(m.Type)
}

func (m *MockAccess) SetType(t runtime.Type) {
	m.Type = t.String()
}

func (m *MockAccess) DeepCopyTyped() runtime.Typed {
	cp := *m
	return &cp
}

func TestResource_GettersAndSetters(t *testing.T) {
	acc := &MockAccess{Type: "mockType"}

	r, err := typed.NewArtifact[*MockAccess](&untyped.Resource{})
	assert.Error(t, err)

	r, err = typed.NewArtifact[*MockAccess](&untyped.Resource{Access: acc})
	assert.NoError(t, err)

	r.Base().SetType("resourceType")
	r.Typed().SetAccess(acc)

	assert.Equal(t, "resourceType", r.Base().GetType())
	assert.Equal(t, acc, r.Base().GetAccess())
	assert.Equal(t, "mockType", r.Base().GetAccess().GetType().String())

	assert.Equal(t, acc, r.Typed().GetAccess())
}

// Simulate a custom JSON field by using Labels or a dummy map (since untyped.Resource has fixed fields).
// Here we embed the custom field through an auxiliary struct.
type WithCustom struct {
	untyped.Resource `json:",inline"`
	Custom           string `json:"custom"`
}
