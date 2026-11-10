package writer

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/universe"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// WriteSchemaJSON writes the final JSON schema for a single TypeInfo.
func WriteSchemaJSON(ti *universe.TypeInfo, schema *jsonschemagen.JSONSchemaDraft202012) error {
	outDir := filepath.Join(filepath.Dir(ti.FilePath), "schemas")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// EscapeForHTML keeps <, > and & escaped as \u003c, \u003e and \u0026, which is what
	// encoding/json v1 did unconditionally and what the committed schema files contain.
	raw, err := json.Marshal(schema, runtime.JSONOptions, jsontext.WithIndent("  "), jsontext.EscapeForHTML(true))
	if err != nil {
		return err
	}

	out := filepath.Join(outDir, ti.Key.TypeName+".schema.json")
	return os.WriteFile(out, raw, 0o600)
}
