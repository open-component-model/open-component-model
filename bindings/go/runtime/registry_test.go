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

func TestRegistry_Decode(t *testing.T) {
	r := require.New(t)
	typ := NewType("test", "v1", "type")
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
