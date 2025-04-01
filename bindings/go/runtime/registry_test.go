package runtime

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

type TestType struct {
	Type  Type   `json:"type"`
	Value string `json:"value"`
}

func (t *TestType) GetType() Type {
	return t.Type
}

func (t *TestType) SetType(typ Type) {
	t.Type = typ
}

func (t *TestType) DeepCopyTyped() Typed {
	return &TestType{
		Type:  t.Type,
		Value: t.Value,
	}
}

func TestRegistry_Decode(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	raw := &Raw{Type: typ, Data: []byte(`{"type": "test.type", "value": "foo"}`)}

	parsed := &TestType{}
	r.NoError(registry.Convert(raw, parsed))
	r.Equal(parsed.Value, "foo")

	r.NoError(registry.Convert(&TestType{Type: typ, Value: "bar"}, parsed))
	r.Equal(parsed.Value, "bar")

	parsed2, err := registry.NewObject(typ)
	r.NoError(err)

	r.NoError(registry.Clone().Convert(raw, parsed2))
	r.IsType(&TestType{}, parsed2)
	r.Equal(parsed2.(*TestType).Value, "foo")

	parsed3, err := registry.Clone().NewObject(typ)
	r.NoError(err)
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), parsed3))
	r.Equal(parsed3.(*TestType).Value, "foo")

	unknown := NewScheme()
	_, err = unknown.NewObject(typ)
	r.Error(err)

	r.Error(unknown.Decode(bytes.NewReader(raw.Data), parsed3))
	newRaw := &Raw{}
	r.Error(unknown.Decode(bytes.NewReader(raw.Data), newRaw))

	unknown = NewScheme(WithAllowUnknown())
	parsed4, err := unknown.NewObject(typ)
	r.NoError(err)
	r.NoError(unknown.Decode(bytes.NewReader(raw.Data), parsed4))
	r.IsType(&Raw{}, parsed4)
	// Version is not set because it is not part of the raw data
	r.Equal(parsed4.(*Raw).Type.String(), "test.type")
}

func TestRegistry_Convert_WithAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme(WithAllowUnknown())
	raw := &Raw{Type: typ, Data: []byte(`{"type": "test/v1", "value": "foo"}`)}

	// Test Raw → Typed conversion
	parsed := &TestType{}
	r.NoError(registry.Convert(raw, parsed))
	r.Equal(parsed.Value, "foo")
	r.Equal(parsed.Type, typ)

	// Test Typed → Raw conversion
	from := &TestType{Type: typ, Value: "bar"}
	target := &Raw{}
	r.NoError(registry.Convert(from, target))
	r.Equal(target.Type, typ)
	r.JSONEq(`{"type": "test/v1", "value": "bar"}`, string(target.Data))

	// Test Raw → Raw conversion
	target2 := &Raw{}
	r.NoError(registry.Convert(raw, target2))
	r.Equal(raw.Type, target2.Type)
	r.Equal(raw.Data, target2.Data)

	// Test Typed → Typed conversion
	to := &TestType{}
	r.NoError(registry.Convert(from, to))
	r.Equal(from.Value, to.Value)
	r.Equal(from.Type, to.Type)
}
