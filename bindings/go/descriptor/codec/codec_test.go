package codec

import (
	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/golang/runtime"
	"strings"
	"testing"
)

func TestRegistryDecoder_JSONDecode(t *testing.T) {
	typeRegistry := runtime.NewScheme()

	typ := runtime.NewType("customAccess", "v1alpha1")
	type CustomAccess struct {
		runtime.Type `json:",inline"`
		CustomField  string `json:"customField"`
	}

	err := typeRegistry.MustRegister(CustomAccess{}, typ)
	assert.Nil(t, err)

	fac := NewTypedDecoderFactory(typeRegistry, NewJSONDecoder)
	assert.NotNil(t, fac)
	typed, err := fac.NewTypedDecoder(strings.NewReader(`{"type": "customAccess/v1alpha1", "customField": "exampleValue"}`)).Decode()
	assert.Nil(t, err)
	assert.NotNil(t, typed)
	assert.Equal(t, "customAccess", typed.GetType().GetKind())
}

func TestRegistryDecoder_YAMLDecode(t *testing.T) {
	typeRegistry := runtime.NewScheme()

	typ := runtime.NewType("customAccess", "v1alpha1")
	type CustomAccess struct {
		runtime.Type `json:",inline"`
		CustomField  string `json:"customField"`
	}

	err := typeRegistry.MustRegister(CustomAccess{}, typ)
	assert.Nil(t, err)

	fac := NewTypedDecoderFactory(typeRegistry, NewYAMLDecoder)
	assert.NotNil(t, fac)

	typed, err := fac.NewTypedDecoder(strings.NewReader(`
type: customAccess/v1alpha1
customField: exampleValue
`)).Decode()
	assert.Nil(t, err)
	assert.NotNil(t, typed)
	assert.Equal(t, "customAccess", typed.GetType().GetKind())
}
