package access_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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

func TestLegacyDocuments(t *testing.T) {
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)

			parsed, err := runtime.TypeFromString(typ)
			r.NoError(err)

			obj, err := access.Scheme.NewObject(parsed)
			r.NoError(err)

			for _, document := range []string{
				fmt.Sprintf(`{"type":%q,"registry":"https://registry.npmjs.org","package":"@types/node","version":"20.11.5"}`, typ),
				fmt.Sprintf("type: %s\nregistry: https://registry.npmjs.org\npackage: \"@types/node\"\nversion: 20.11.5\n", typ),
			} {
				r.NoError(access.Scheme.Decode(strings.NewReader(document), obj))

				spec := obj.(*v1.NPM)
				r.NoError(spec.Validate())
				r.Equal(typ, spec.Type.String())
				r.Equal("https://registry.npmjs.org", spec.Registry)
				r.Equal("@types/node", spec.Package)
				r.Equal("20.11.5", spec.Version)
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
		r.NoError(schema.Validate(map[string]any{
			"type":     typ,
			"registry": "https://registry.npmjs.org",
			"package":  "lodash",
			"version":  "4.17.21",
		}))
	}
	r.Error(schema.Validate(map[string]any{
		"type":     "NPM/v2",
		"registry": "https://registry.npmjs.org",
		"package":  "lodash",
		"version":  "4.17.21",
	}))
}

func TestOCMV1SerializedDocument(t *testing.T) {
	r := require.New(t)

	data, err := os.ReadFile("testdata/ocmv1.json")
	r.NoError(err)

	var spec v1.NPM
	r.NoError(access.Scheme.Decode(bytes.NewReader(data), &spec))
	r.NoError(spec.Validate())
	r.Equal("npm", spec.Type.String())
	r.Equal("https://registry.npmjs.org/", spec.Registry)
	r.Equal("yargs", spec.Package)
	r.Equal("17.7.2", spec.Version)
}
