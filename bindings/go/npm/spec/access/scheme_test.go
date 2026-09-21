package access_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/npm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// types are the access type names registered by the OCM v1 npm access type.
var types = []string{"NPM/v1", "NPM", "npm", "npm/v1"}

var legacyValues = []struct{ registry, pkg, version string }{
	{"https://registry.npmjs.org", "@types/node", "20.11.5"},
	{"https://registry.npmjs.org", ".unusual package", "latest"},
	{"file:///abs/registry", "_package", "v1.2.3"},
	{"file://relative/path", "scope/name", ">=1.0.0 <2.0.0"},
	{"file://relative/a b%?#/registry", strings.Repeat("a", 215), "arbitrary version"},
}

func TestLegacyDocuments(t *testing.T) {
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)

			parsed, err := runtime.TypeFromString(typ)
			r.NoError(err)

			obj, err := access.Scheme.NewObject(parsed)
			r.NoError(err)

			for _, values := range legacyValues {
				// The documents OCM v1 serialised, in both encodings it used.
				for _, document := range []string{
					fmt.Sprintf(`{"type":%q,"registry":%q,"package":%q,"version":%q}`, typ, values.registry, values.pkg, values.version),
					fmt.Sprintf("type: %s\nregistry: %q\npackage: %q\nversion: %q\n", typ, values.registry, values.pkg, values.version),
				} {
					r.NoError(access.Scheme.Decode(strings.NewReader(document), obj))

					spec := obj.(*v1.NPM)
					r.NoError(spec.Validate())
					r.Equal(typ, spec.Type.String())
					r.Equal(values.registry, spec.Registry)
					r.Equal(values.pkg, spec.Package)
					r.Equal(values.version, spec.Version)
				}
			}
		})
	}

	r := require.New(t)

	_, err := access.Scheme.NewObject(runtime.NewVersionedType("NPM", "v2"))
	r.Error(err)
}

func TestGeneratedSchemaAliases(t *testing.T) {
	r := require.New(t)

	var schemaData any
	r.NoError(json.Unmarshal((&v1.NPM{}).JSONSchema(), &schemaData))

	compiler := jsonschema.NewCompiler()
	r.NoError(compiler.AddResource("https://example.invalid/npm.schema.json", schemaData))

	schema, err := compiler.Compile("https://example.invalid/npm.schema.json")
	r.NoError(err)

	for _, typ := range types {
		for _, values := range legacyValues {
			r.NoError(schema.Validate(map[string]any{
				"type":     typ,
				"registry": values.registry,
				"package":  values.pkg,
				"version":  values.version,
			}))
		}
	}
	r.Error(schema.Validate(map[string]any{
		"type":     "NPM/v2",
		"registry": "https://registry.npmjs.org",
		"package":  "lodash",
		"version":  "4.17.21",
	}))
}
