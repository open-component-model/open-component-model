package jsonschemagen_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
)

func TestSchemaOrBoolMarshalJSONWithBoolTrue(t *testing.T) {
	sb := jsonschemagen.SchemaOrBool{Bool: jsonschemagen.Ptr(true)}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte("true"), data)
}

func TestSchemaOrBoolMarshalJSONWithBoolFalse(t *testing.T) {
	sb := jsonschemagen.SchemaOrBool{Bool: jsonschemagen.Ptr(false)}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte("false"), data)
}

func TestSchemaOrBoolMarshalJSONWithSchema(t *testing.T) {
	schema := &jsonschemagen.Schema{Type: "string"}
	sb := jsonschemagen.SchemaOrBool{Schema: schema}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte(`{"type":"string"}`), data)
}
