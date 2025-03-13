package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type TestType struct {
	Type  Type   `json:"type"`
	Value string `json:"value"`
}

func TestRegistry_Decode(t *testing.T) {
	typ := NewUngroupedVersionedType("test.type", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	r := require.New(t)

	parsed := &TestType{}
	r.NoError(registry.Convert(&Raw{Type: typ, Data: []byte(`{"type": "test.type", "value": "foo"}`)}, parsed))
	r.Equal(parsed.Value, "foo")

	r.NoError(registry.Convert(&TestType{Type: typ, Value: "bar"}, parsed))
	r.Equal(parsed.Value, "bar")
}
