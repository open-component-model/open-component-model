package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Define a simple runtime.Typed for testing
type TestPluginType struct {
	Name    string                 `json:"name"`
	Version string                 `json:"version"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

func (t TestPluginType) GetType() runtime.Type {
	return runtime.Type{
		Name:    t.Name,
		Version: t.Version,
	}
}

func (t TestPluginType) SetType(t2 runtime.Type) {
	t.Name = t2.Name
	t.Version = t2.Version
}

func (t TestPluginType) DeepCopyTyped() runtime.Typed {
	return &TestPluginType{
		Name:    t.Name,
		Version: t.Version,
		Enabled: t.Enabled,
		Config:  t.Config,
	}
}

type TestPluginWrongType struct {
	Name    string
	Version string
	Enabled string
}

func (t TestPluginWrongType) GetType() runtime.Type {
	return runtime.Type{
		Name:    t.Name,
		Version: t.Version,
	}
}

func (t TestPluginWrongType) SetType(t2 runtime.Type) {
	t.Name = t2.Name
	t.Version = t2.Version
}

func (t TestPluginWrongType) DeepCopyTyped() runtime.Typed {
	return &TestPluginWrongType{
		Name:    t.Name,
		Version: t.Version,
		Enabled: t.Enabled,
	}
}

func TestValidatePlugin_ValidSchemaAndType(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
			"version": { "type": "string" },
			"enabled": { "type": "boolean" }
		},
		"required": ["name", "version", "enabled"]
	}`)

	pluginType := TestPluginType{
		Name:    "my-plugin",
		Version: "v1.0.0",
		Enabled: true,
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.NoError(t, err)
	assert.True(t, valid)
}

func TestValidatePlugin_InvalidTypeAgainstSchemaMissingRequiredField(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
			"version": { "type": "string" },
			"enabled": { "type": "string" },
			"extra": { "type": "string" }
		},
		"required": ["extra"]
	}`)

	pluginType := TestPluginType{
		Name: "my-plugin",
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "extra")
}

func TestValidatePlugin_InvalidTypeAgainstSchemaWrongType(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
			"version": { "type": "string" },
			"enabled": { "type": "boolean" }
		},
		"required": ["name", "version", "enabled"]
	}`)

	pluginType := TestPluginWrongType{
		Name:    "my-plugin",
		Version: "v1.0.0",
		Enabled: "true",
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "boolean")
}

func TestValidatePlugin_ValidTypeWithOptionalField(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
			"version": { "type": "string" },
			"enabled": { "type": "boolean" },
			"config": { "type": "object" }
		},
		"required": ["name", "version", "enabled"]
	}`)

	pluginType := TestPluginType{
		Name:    "my-plugin",
		Version: "v1.0.0",
		Enabled: true,
		Config:  map[string]interface{}{"setting": "value"},
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.NoError(t, err)
	assert.True(t, valid)
}

func TestValidatePlugin_InvalidSchemaMalformedJSON(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
		},
		"required": ["name" // Missing closing brace
	`)

	pluginType := TestPluginType{
		Name: "my-plugin",
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "invalid character '}' looking for beginning of object key string")
}

func TestValidatePlugin_SchemaCompilationError(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "unknown" } // Invalid type keyword
		},
		"required": ["name"]
	}`)

	pluginType := TestPluginType{
		Name: "my-plugin",
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "invalid character '/' after object key:value pair")
}

func TestValidatePlugin_ComplexSchemaAndType(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": { "type": "string" },
			"version": { "type": "string", "pattern": "^v\\d+\\.\\d+\\.\\d+$" },
			"enabled": { "type": "boolean" },
			"config": {
				"type": "object",
				"properties": {
					"host": { "type": "string", "format": "hostname" },
					"port": { "type": "integer", "minimum": 1, "maximum": 65535 }
				},
				"required": ["host", "port"]
			},
			"tags": {
				"type": "array",
				"items": { "type": "string" }
			}
		},
		"required": ["name", "version", "enabled"]
	}`)

	pluginType := TestPluginType{
		Name:    "complex-plugin",
		Version: "v2.1.0",
		Enabled: true,
		Config: map[string]interface{}{
			"host": "localhost",
			"port": 8080,
		},
		// Tags are optional according to the schema
	}

	valid, err := ValidatePlugin(pluginType, schema)
	assert.NoError(t, err)
	assert.True(t, valid)

	invalidPluginType := TestPluginType{
		Name:    "complex-plugin",
		Version: "2.1.0", // Invalid version format
		Enabled: true,
		Config: map[string]interface{}{
			"host": "localhost",
			"port": 0, // Invalid port
		},
	}

	validInvalid, errInvalid := ValidatePlugin(invalidPluginType, schema)
	assert.Error(t, errInvalid)
	assert.False(t, validInvalid)
	assert.Contains(t, errInvalid.Error(), "version")
	assert.Contains(t, errInvalid.Error(), "port")
}
