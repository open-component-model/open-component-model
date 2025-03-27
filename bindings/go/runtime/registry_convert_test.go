package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestType struct {
	Type runtime.Type `json:"type"`
	Foo  string       `json:"foo"`
}

func (t *TestType) SetType(typ runtime.Type) {
	t.Type = typ
}

func (t *TestType) GetType() runtime.Type {
	return t.Type
}

func (t *TestType) DeepCopyTyped() runtime.Typed {
	return &TestType{
		Type: t.Type,
		Foo:  t.Foo,
	}
}

var _ runtime.Typed = &TestType{}

type TestType2 struct {
	Type runtime.Type `json:"type"`
	Foo  string       `json:"foo"`
}

func (t *TestType2) GetType() runtime.Type {
	return t.Type
}

func (t *TestType2) DeepCopyTyped() runtime.Typed {
	return &TestType2{
		Type: t.Type,
		Foo:  t.Foo,
	}
}

func (t *TestType2) SetType(typ runtime.Type) {
	t.Type = typ
}

var _ runtime.Typed = &TestType2{}

func TestScheme_Convert_From_Raw_And_Back(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)
	r := runtime.Raw{Type: typ, Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`)}

	var val TestType
	require.NoError(t, scheme.Convert(&r, &val))
	require.Equal(t, "bar", val.Foo)

	var r2 runtime.Raw
	require.NoError(t, scheme.Convert(&val, &r2))
	require.JSONEq(t, string(r.Data), string(r2.Data))

	var r3 runtime.Raw
	require.NoError(t, scheme.Convert(&r2, &r3))
	require.JSONEq(t, string(r.Data), string(r3.Data))

	var val2 TestType
	require.NoError(t, scheme.Convert(&r3, &val2))
	require.Equal(t, typ, val2.Type)
}

func TestConvert_RawToRaw(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)
	original := &runtime.Raw{Type: typ, Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`)}
	target := &runtime.Raw{}

	err := scheme.Convert(original, target)
	require.NoError(t, err)
	assert.Equal(t, original.Type, target.Type)
	assert.Equal(t, original.Data, target.Data)
}

func TestConvert_RawToTyped(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)
	raw := &runtime.Raw{Type: typ, Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`)}

	out := &TestType{}

	err := scheme.Convert(raw, out)
	require.NoError(t, err)
	assert.Equal(t, "bar", out.Foo)
}

func TestConvert_TypedToRaw(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)

	from := &TestType{Type: typ, Foo: "bar"}
	raw := &runtime.Raw{}

	err := scheme.Convert(from, raw)
	require.NoError(t, err)

	assert.Equal(t, typ, raw.Type)

	var parsed TestType
	err = json.Unmarshal(raw.Data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, from.Foo, parsed.Foo)
}

func TestConvert_TypedToTyped(t *testing.T) {
	s := runtime.NewScheme()
	s.MustRegister(&TestType{}, "v1")

	from := &TestType{Foo: "bar"}
	to := &TestType{}

	err := s.Convert(from, to)
	require.NoError(t, err)

	assert.Equal(t, "bar", to.Foo)
	assert.NotSame(t, from, to)
	assert.NotEmpty(t, to.Type)
}

func TestConvert_Errors(t *testing.T) {
	proto := &TestType{}

	t.Run("nil from", func(t *testing.T) {
		scheme := runtime.NewScheme()
		err := scheme.Convert(nil, proto)
		assert.Error(t, err)
	})

	t.Run("nil into", func(t *testing.T) {
		scheme := runtime.NewScheme()
		err := scheme.Convert(proto, nil)
		assert.Error(t, err)
	})

	t.Run("unregistered type (Raw → Typed)", func(t *testing.T) {
		scheme := runtime.NewScheme()
		r := runtime.Raw{Type: runtime.NewUngroupedVersionedType("TestType", "v1"),
			Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`)}

		err := scheme.Convert(&r, &TestType{})
		assert.Error(t, err)
	})

	t.Run("unregistered type (Typed → Raw)", func(t *testing.T) {
		scheme := runtime.NewScheme()
		typ := runtime.NewUngroupedVersionedType("TestType", "v1")
		r := runtime.Raw{}

		err := scheme.Convert(&TestType{Type: typ, Foo: "bar"}, &r)
		assert.Error(t, err)
	})

	t.Run("incompatible types in Typed → Typed", func(t *testing.T) {
		proto := &TestType{}
		scheme := runtime.NewScheme()
		scheme.MustRegister(proto, "v1")
		typ := scheme.MustTypeForPrototype(proto)
		proto.Type = typ

		proto2 := &TestType2{}
		scheme.MustRegister(proto2, "v1")
		typ2 := scheme.MustTypeForPrototype(proto2)
		proto2.Type = typ2

		err := scheme.Convert(proto, proto2)
		assert.Error(t, err)
	})
}
