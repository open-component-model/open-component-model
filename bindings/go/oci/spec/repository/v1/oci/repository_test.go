package oci

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRepository_ComponentVersionLayout_JSON(t *testing.T) {
	r := Repository{
		Type:                   runtime.NewVersionedType(Type, "v1"),
		BaseUrl:                "localhost:5001",
		ComponentVersionLayout: "normalized",
	}
	b, err := json.Marshal(&r)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"componentVersionLayout":"normalized"`)

	var back Repository
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, "normalized", back.ComponentVersionLayout)
}

// TestRepository_ComponentVersionLayout_SchemaAccepts ensures the generated JSON Schema
// declares the componentVersionLayout property. The schema sets additionalProperties:false,
// so without this property a spec carrying componentVersionLayout would be rejected during
// validation. Asserting the property exists in the schema guarantees acceptance.
func TestRepository_ComponentVersionLayout_SchemaAccepts(t *testing.T) {
	schema := Repository{}.JSONSchema()
	require.NotEmpty(t, schema)

	var parsed struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	require.NoError(t, json.Unmarshal(schema, &parsed))
	require.False(t, parsed.AdditionalProperties,
		"schema is expected to forbid additional properties, making the property declaration load-bearing")
	require.Contains(t, parsed.Properties, "componentVersionLayout",
		"generated schema must declare componentVersionLayout so specs setting it pass validation")
}
