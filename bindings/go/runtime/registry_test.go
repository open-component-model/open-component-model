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

func TestRegistry_Decode_With_Type_Mismatch_Defaulting_On_Raw(t *testing.T) {
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
	// forcefully empty the type, because new object defaults to the correct type
	r.NoError(err)
	parsed3.SetType(NewUnversionedType(""))
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

func TestRegistry_Decode_UnknownType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme(WithAllowUnknown())
	raw := &Raw{Type: typ, Data: []byte(`{"type": "unknown/v1", "value": "foo"}`)}

	// Test decoding into a Raw object
	parsed := &Raw{}
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), parsed))
	r.Equal(parsed.Type, typ)
	r.JSONEq(string(raw.Data), string(parsed.Data))

	// Test decoding into a typed object with unknown type
	typed := &TestType{}
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), typed))
	r.Equal(typed.Type, typ)
	r.Equal(typed.Value, "foo")
}

func TestRegistry_Decode_UnknownType_WithoutAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme() // Without WithAllowUnknown()
	raw := &Raw{Type: typ, Data: []byte(`{"type": "unknown/v1", "value": "foo"}`)}

	// Test decoding into a Raw object
	parsed := &Raw{}
	r.Error(registry.Decode(bytes.NewReader(raw.Data), parsed))

	// Test decoding into a typed object with unknown type
	typed := &TestType{}
	r.Error(registry.Decode(bytes.NewReader(raw.Data), typed))
}

func TestRegistry_Decode_ErrorCases(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test empty input data
	parsed := &TestType{}
	err := registry.Decode(bytes.NewReader([]byte{}), parsed)
	r.Error(err)
	r.Contains(err.Error(), "cannot decode empty input data")

	// Test invalid JSON input
	invalidJSON := []byte(`{"type": "test/v1", "value": "foo"`) // Missing closing brace
	err = registry.Decode(bytes.NewReader(invalidJSON), parsed)
	r.Error(err)
	r.Contains(err.Error(), "failed to unmarshal raw")

	// Test invalid YAML input
	invalidYAML := []byte("type: test/v1\nvalue: foo\n  extra: invalid") // Invalid indentation
	err = registry.Decode(bytes.NewReader(invalidYAML), parsed)
	r.Error(err)
	r.Contains(err.Error(), "failed to unmarshal raw")

	// Test type mismatch after decoding
	validData := []byte(`{"type": "test/v2", "value": "foo"}`) // Different version than registered
	parsed.SetType(typ)                                        // Set initial type
	err = registry.Decode(bytes.NewReader(validData), parsed)
	r.Error(err)
	r.Contains(err.Error(), "expected type")
}

func TestRegistry_Default_UnknownType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme(WithAllowUnknown())

	// Test defaulting a Raw object
	raw := &Raw{Type: typ}
	updated, err := registry.DefaultType(raw)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already set

	// Test defaulting a typed object with unknown type
	typed := &TestType{Type: typ}
	updated, err = registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already set
}

func TestRegistry_Default_UnknownType_WithoutAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme() // Without WithAllowUnknown()

	// Test defaulting a Raw object
	raw := &Raw{Type: typ}
	_, err := registry.DefaultType(raw)
	r.Error(err)

	// Test defaulting a typed object with unknown type
	typed := &TestType{Type: typ}
	_, err = registry.DefaultType(typed)
	r.Error(err)
}

func TestRegistry_Default_EmptyType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test defaulting a typed object with empty type
	typed := &TestType{}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should be updated since it was empty
	r.Equal(typed.Type, typ)
}

func TestRegistry_Default_AlreadySet(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test defaulting a typed object with correct type already set
	typed := &TestType{Type: typ}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it was already correct
	r.Equal(typed.Type, typ)
}

func TestRegistry_Default_DifferentRegisteredType(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	// Test defaulting a typed object with different registered type
	typed := &TestType{Type: typ2}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already a valid registered type
	r.Equal(typed.Type, typ2)
}

func TestRegistry_Default_MultipleRegisteredTypesWithoutTypeSet(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	// Test defaulting a typed object with different registered type
	typed := &TestType{}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should not be updated since it's already a valid registered type
	r.Equal(typed.Type, typ1)
}

func TestRegistry_Default_UnregisteredType(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1)

	// Test defaulting a typed object with unregistered type gets overwritten with the registered type
	typed := &TestType{Type: typ2}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should be updated since it was unregistered
}

func TestRegistry_MultipleTypes_With_Alias(t *testing.T) {
	r := require.New(t)
	def := NewVersionedType("test1", "v1")
	alias := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, def, alias)
	r.ErrorContains(registry.RegisterWithAlias(&TestType{}, def), "already registered as default")
	r.ErrorContains(registry.RegisterWithAlias(&TestType{}, alias), "already registered as alias")
}

func TestRegistry_NewObject_Based_On_Alias(t *testing.T) {
	r := require.New(t)
	def := NewVersionedType("test1", "v1")
	alias := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, def, alias)

	obj, err := registry.NewObject(def)
	r.NoError(err)
	r.Equal(obj.GetType(), def)
	obj, err = registry.NewObject(alias)
	r.NoError(err)
	r.Equal(obj.GetType(), alias)
}
