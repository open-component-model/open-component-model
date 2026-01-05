package jsonschema

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestExtract_Robust(t *testing.T) {
	schema := `{
		"title": "ComplexType",
		"definitions": {
			"Common": {
				"properties": {
					"id": { "type": "string", "description": "Common ID" }
				}
			}
		},
		"allOf": [
			{ "$ref": "#/definitions/Common" }
		],
		"oneOf": [
			{
				"properties": {
					"kind": { "type": "string", "enum": ["A"] },
					"specA": { 
						"type": "object",
						"properties": {
							"value": { "type": "integer" }
						}
					}
				}
			},
			{
				"properties": {
					"kind": { "type": "string", "enum": ["B"] },
					"specB": { "type": "string" }
				}
			}
		]
	}`

	doc, err := Extract("complex", []byte(schema))
	assert.NoError(t, err)
	assert.Equal(t, "ComplexType", doc.Title)

	// Check for merged and flattened properties
	paths := []string{}
	for _, p := range doc.Properties {
		paths = append(paths, p.Path)
	}

	assert.Contains(t, paths, "id")
	assert.Contains(t, paths, "kind")
	assert.Contains(t, paths, "specA")
	assert.Contains(t, paths, "specA.value")
	assert.Contains(t, paths, "specB")

	// Verify details
	for _, p := range doc.Properties {
		if p.Path == "id" {
			assert.Equal(t, "Common ID", p.Description)
		}
		if p.Path == "specA.value" {
			assert.Equal(t, "integer", p.Type)
		}
	}
}

type mockIntrospectable struct {
	runtime.Type `json:",inline"`
	schema       string
}

func (m *mockIntrospectable) SetType(t runtime.Type) { m.Type = t }
func (m *mockIntrospectable) GetType() runtime.Type  { return m.Type }
func (m *mockIntrospectable) DeepCopyTyped() runtime.Typed {
	return &mockIntrospectable{Type: m.Type, schema: m.schema}
}

func (m *mockIntrospectable) JSONSchema() []byte {
	return []byte(m.schema)
}

func TestFromType(t *testing.T) {
	scheme := runtime.NewScheme()
	typ := runtime.NewVersionedType("Mock", "v1")
	schema := `{
		"title": "MockType",
		"properties": {
			"field": { "type": "string" }
		}
	}`

	scheme.MustRegisterWithAlias(&mockIntrospectable{schema: schema}, typ)

	doc, err := FromType(scheme, typ)
	assert.NoError(t, err)
	assert.NotNil(t, doc)
	assert.Equal(t, "MockType", doc.Title)
	assert.Equal(t, "field", doc.Properties[0].Name)
}
