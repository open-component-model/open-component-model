package writer

import (
	"encoding/json"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/universe"
)

// WriteSchemaJSON writes the final JSON schema for a single TypeInfo.
func WriteSchemaJSON(ti *universe.TypeInfo, schema *jsonschemagen.JSONSchemaDraft202012) error {
	outDir := filepath.Join(filepath.Dir(ti.FilePath), "schemas")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}

	out := filepath.Join(outDir, ti.Key.TypeName+".schema.json")
	return os.WriteFile(out, raw, 0o600)
}
