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
		r := runtime.Raw{
			Type: runtime.NewVersionedType("TestType", "v1"),
			Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`),
		}

		err := scheme.Convert(&r, &TestType{})
		assert.Error(t, err)
	})

	t.Run("unregistered type (Typed → Raw)", func(t *testing.T) {
		scheme := runtime.NewScheme()
		typ := runtime.NewVersionedType("TestType", "v1")
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

func TestConvert_UnstructuredToTyped(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)

	un := &runtime.Unstructured{
		Data: map[string]any{
			"type": typ.String(),
			"foo":  "bar",
		},
	}

	out := &TestType{}
	err := scheme.Convert(un, out)
	require.NoError(t, err)
	assert.Equal(t, "bar", out.Foo)
	assert.Equal(t, typ, out.Type)
}

func TestConvert_UnstructuredToRaw(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)

	un := &runtime.Unstructured{
		Data: map[string]any{
			"type": typ.String(),
			"foo":  "bar",
		},
	}

	raw := &runtime.Raw{}
	err := scheme.Convert(un, raw)
	require.NoError(t, err)

	var parsed TestType
	require.NoError(t, json.Unmarshal(raw.Data, &parsed))
	assert.Equal(t, "bar", parsed.Foo)
	assert.Equal(t, typ, parsed.Type)
}

func TestConvert_UnstructuredToTyped_UnregisteredType(t *testing.T) {
	scheme := runtime.NewScheme()

	un := &runtime.Unstructured{
		Data: map[string]any{
			"type": "UnknownType/v1",
			"foo":  "bar",
		},
	}

	out := &TestType{}
	err := scheme.Convert(un, out)
	assert.Error(t, err)
}

func TestConvert_UnstructuredToTyped_AllowUnknown(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	un := &runtime.Unstructured{
		Data: map[string]any{
			"type": "UnknownType/v1",
			"foo":  "bar",
		},
	}

	out := &TestType{}
	err := scheme.Convert(un, out)
	require.NoError(t, err)
	assert.Equal(t, "bar", out.Foo)
}

func TestConvert_TypedToUnstructured(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegister(&TestType{}, "v1")

	from := &TestType{
		Type: runtime.NewVersionedType("TestType", "v1"),
		Foo:  "bar",
	}
	into := &runtime.Unstructured{Data: make(map[string]any)}

	err := scheme.Convert(from, into)
	assert.Error(t, err, "Typed → Unstructured is not supported via reflection assignment")
}

func TestConvert_UnstructuredRoundTrip(t *testing.T) {
	proto := &TestType{}
	scheme := runtime.NewScheme()
	scheme.MustRegister(proto, "v1")
	typ := scheme.MustTypeForPrototype(proto)

	// Start with Unstructured
	un := &runtime.Unstructured{
		Data: map[string]any{
			"type": typ.String(),
			"foo":  "baz",
		},
	}

	// Unstructured → Typed
	typed := &TestType{}
	require.NoError(t, scheme.Convert(un, typed))
	assert.Equal(t, "baz", typed.Foo)
	assert.Equal(t, typ, typed.Type)

	// Typed → Raw
	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(typed, raw))
	assert.Equal(t, typ, raw.Type)

	// Raw → Typed (back again)
	typed2 := &TestType{}
	require.NoError(t, scheme.Convert(raw, typed2))
	assert.Equal(t, "baz", typed2.Foo)
	assert.Equal(t, typ, typed2.Type)
}

func TestConvert_AllowUnknown(t *testing.T) {
	typ := runtime.NewVersionedType("TestType", "v1")
	raw := &runtime.Raw{
		Type: typ,
		Data: []byte(`{"type": "TestType/v1", "foo": "bar"}`),
	}

	t.Run("Raw → Typed with allowUnknown", func(t *testing.T) {
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		out := &TestType{}
		err := scheme.Convert(raw, out)
		require.NoError(t, err)
		assert.Equal(t, "bar", out.Foo)
	})

	t.Run("Typed → Raw with allowUnknown", func(t *testing.T) {
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		from := &TestType{Type: typ, Foo: "bar"}
		target := &runtime.Raw{}
		err := scheme.Convert(from, target)
		require.NoError(t, err)
		assert.Equal(t, typ, target.Type)
		assert.JSONEq(t, string(raw.Data), string(target.Data))
	})

	t.Run("Raw → Raw with allowUnknown", func(t *testing.T) {
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		target := &runtime.Raw{}
		err := scheme.Convert(raw, target)
		require.NoError(t, err)
		assert.Equal(t, raw.Type, target.Type)
		assert.Equal(t, raw.Data, target.Data)
	})

	t.Run("Typed → Typed with allowUnknown", func(t *testing.T) {
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		from := &TestType{Type: typ, Foo: "bar"}
		to := &TestType{}
		err := scheme.Convert(from, to)
		require.NoError(t, err)
		assert.Equal(t, "bar", to.Foo)
		assert.Equal(t, typ, to.Type)
	})
}
