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

	"ocm.software/open-component-model/bindings/go/git/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLegacyDocuments(t *testing.T) {
	for _, typ := range []string{"Git/v1", "Git", "git", "git/v1alpha1", "Git/v1alpha1"} {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)

			parsed, err := runtime.TypeFromString(typ)
			r.NoError(err)

			obj, err := access.Scheme.NewObject(parsed)
			r.NoError(err)

			for _, document := range []string{
				fmt.Sprintf(`{"type":%q,"repository":"https://github.com/open-component-model/ocm","ref":"refs/heads/main","commit":"0123456789abcdef0123456789abcdef01234567"}`, typ),
				fmt.Sprintf("type: %s\nrepository: git@github.com:open-component-model/ocm.git\nref: refs/heads/main\ncommit: 0123456789abcdef0123456789abcdef01234567\n", typ),
			} {
				r.NoError(access.Scheme.Decode(strings.NewReader(document), obj))

				spec := obj.(*v1.Git)
				r.NoError(spec.Validate())
				r.Equal(typ, spec.Type.String())
				r.Equal("refs/heads/main", spec.Ref)
				r.Equal("0123456789abcdef0123456789abcdef01234567", spec.Commit)
			}
		})
	}

	r := require.New(t)

	_, err := access.Scheme.NewObject(runtime.NewVersionedType("git", "v1"))
	r.Error(err)
}

func TestGeneratedSchemaAliases(t *testing.T) {
	r := require.New(t)

	var schemaData any
	r.NoError(json.Unmarshal((&v1.Git{}).JSONSchema(), &schemaData))

	compiler := jsonschema.NewCompiler()
	r.NoError(compiler.AddResource("https://example.invalid/git.schema.json", schemaData))

	schema, err := compiler.Compile("https://example.invalid/git.schema.json")
	r.NoError(err)

	for _, typ := range []string{"Git/v1", "Git", "git", "git/v1alpha1", "Git/v1alpha1"} {
		r.NoError(schema.Validate(map[string]any{"type": typ, "repository": "https://example.com/repo", "ref": "main"}))
	}
	r.Error(schema.Validate(map[string]any{"type": "git/v1", "repository": "https://example.com/repo"}))
}

func TestOCMV1SerializedDocument(t *testing.T) {
	r := require.New(t)

	data, err := os.ReadFile("testdata/ocmv1.json")
	r.NoError(err)

	var spec v1.Git
	r.NoError(access.Scheme.Decode(bytes.NewReader(data), &spec))
	r.NoError(spec.Validate())
	r.Equal("git", spec.Type.String())
	r.Equal("refs/heads/main", spec.Ref)
	r.Equal("8a06d21da930c1ac2051f2e40a58b9a01673fa04", spec.Commit)
}
