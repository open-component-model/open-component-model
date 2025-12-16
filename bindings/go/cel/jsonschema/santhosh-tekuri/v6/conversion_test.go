package jsonschema_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"testing"

	"github.com/google/cel-go/cel"
	stjsonschemav6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/provider"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

var rootDir string

func init() {
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		rootDir = strings.TrimSpace(string(out))
	}
}

func TestSchemas(t *testing.T) {
	if rootDir == "" {
		t.Skip("unable to determine root dir")
	}
	root, err := os.OpenRoot(rootDir)
	require.NoError(t, err)

	tests := []struct {
		file      string
		verify    func(t *testing.T, decl *jsonschema.DeclType)
		exprTests []struct {
			expr  string
			parse IssueAssertionFunc
			check IssueAssertionFunc
		}
	}{
		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/Config.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.Equal(t, "ocm__dot__software__slash__open__dash__component__dash__model__slash__bindings__slash__go__slash__rsa__slash__signing__slash__v1alpha1__slash__schemas__slash__Config__dot__schema__dot__json", decl.TypeName())
				require.True(t, decl.IsObject())
				require.Equal(t, []string{"type"}, decl.Required())
				require.Len(t, decl.Fields, 3)

				typ := decl.Fields["type"]
				require.Len(t, typ.EnumValues(), 2)

				sa := decl.Fields["signatureAlgorithm"]
				require.Equal(t, "string", sa.Type.TypeName())
				require.Len(t, sa.EnumValues(), 2)
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance.type", NoIssues, NoIssues},
				{"instance.signatureAlgorithm", NoIssues, NoIssues},
				{"instance.signatureEncodingPolicy", NoIssues, NoIssues},
				{"instance.unknown", NoIssues, IssuesExist},
			},
		},
		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/Formats.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.True(t, decl.IsObject())
				require.Contains(t, decl.Fields, "duration")
				require.Contains(t, decl.Fields, "date")
				require.Contains(t, decl.Fields, "datetime")
				require.Contains(t, decl.Fields, "binary")

				require.Equal(t, "string", decl.Fields["duration"].Type.TypeName())
				require.Equal(t, "string", decl.Fields["date"].Type.TypeName())
				require.Equal(t, "string", decl.Fields["datetime"].Type.TypeName())
				require.Equal(t, "string", decl.Fields["binary"].Type.TypeName())

				enumFmt := decl.Fields["enumFmt"]
				require.Equal(t, "string", enumFmt.Type.TypeName())
				require.Len(t, enumFmt.EnumValues(), 2)
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance.duration", NoIssues, NoIssues},
				{"instance.date", NoIssues, NoIssues},
				{"instance.datetime", NoIssues, NoIssues},
				{"instance.binary", NoIssues, NoIssues},
				{"instance.enumFmt == \"2020-01-01T00:00:00Z\"", NoIssues, NoIssues},
				{"instance.enumFmt == 123", NoIssues, IssuesExist},
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/MapAdditionalProps.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				// OUTER OBJECT
				require.True(t, decl.IsMap()) // outer is a map
				require.Empty(t, decl.Fields) // map has no named struct fields

				// INNER OBJECT
				elem := decl.ElemType
				require.True(t, elem.IsObject())

				require.Contains(t, elem.Fields, "value")
				require.Contains(t, elem.Fields, "flags")

				require.Equal(t, "double", elem.Fields["value"].Type.TypeName())
				require.Equal(t, "list", elem.Fields["flags"].Type.TypeName())
			},

			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				// ---- MAP KEY ACCESS (dot or bracket) ----
				{`instance["foo"]`, NoIssues, NoIssues},
				{`instance.foo`, NoIssues, NoIssues},
				{`instance["foo"].value`, NoIssues, NoIssues},
				{`instance["foo"].flags`, NoIssues, NoIssues},
				{`instance["foo"].flags[0]`, NoIssues, NoIssues},
				{`instance.foo.value`, NoIssues, NoIssues},
				{`instance.foo.flags`, NoIssues, NoIssues},
				{`instance.foo.flags[0]`, NoIssues, NoIssues},
				// This must fail, because inner object only has "value" and "flags"
				{`instance["foo"].other`, NoIssues, IssuesExist},
				{`instance.foo.other`, NoIssues, IssuesExist},
				// dot access to unknown map key
				{`instance.unknownKey`, NoIssues, NoIssues},
				{`instance.unknownKey.value`, NoIssues, NoIssues},
				{`instance.unknownKey.flags[0]`, NoIssues, NoIssues},
				// dot access to existing fields
				{`instance.value`, NoIssues, NoIssues},
				{`instance.flags`, NoIssues, NoIssues},
				{`instance[0]`, NoIssues, IssuesExist},
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/RefOnly.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.Equal(t, "int", decl.TypeName())
				require.False(t, decl.IsObject())
				require.Equal(t, "int", decl.CelType().TypeName())
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance + 1", NoIssues, NoIssues},
				{"instance == \"abc\"", NoIssues, IssuesExist},
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/OpenObject.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.True(t, decl.IsMap())
				require.Empty(t, decl.Fields)
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance.foo", NoIssues, NoIssues},     // Dyn
				{"instance.foo.bar", NoIssues, NoIssues}, // Dyn
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/EnumConstOneOf.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.Equal(t, "test__slash__EnumConstOneOf__dot__schema__dot__json", decl.TypeName())
				require.Len(t, decl.Fields, 1)
				field := decl.Fields["custom"]
				require.Equal(t, "string", field.Type.TypeName())
				require.Len(t, field.EnumValues(), 3) // A, B
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance.custom == \"A\"", NoIssues, NoIssues},
				{"instance.custom == \"B\"", NoIssues, NoIssues},
				{"instance.custom == \"C\"", NoIssues, NoIssues},

				// TODO(currently these enums are accepted even though they could be statically checked)
				// {"instance.custom == \"X\"", NoIssues, IssuesExist},
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/RequiredDefaults.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				require.True(t, decl.IsObject())
				require.Contains(t, decl.Fields, "a")
				require.Contains(t, decl.Fields, "b")
				require.Contains(t, decl.Fields, "d")

				// b has a default → required but no min-size contribution
				b := decl.Fields["b"]
				require.NotNil(t, b.DefaultValue())
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				{"instance.a", NoIssues, NoIssues},
				{"instance.b", NoIssues, NoIssues},
				{"instance.d.x", NoIssues, NoIssues},
				{"instance.d.y == 1.25", NoIssues, NoIssues},
			},
		},

		{
			file: "bindings/go/cel/jsonschema/santhosh-tekuri/v6/testdata/ComplexMixed.schema.json",
			verify: func(t *testing.T, decl *jsonschema.DeclType) {
				// ROOT OBJECT ----------------------------------
				require.True(t, decl.IsObject())
				require.ElementsMatch(t, []string{"kind", "metadata"}, decl.Required())
				require.Contains(t, decl.Fields, "kind")
				require.Contains(t, decl.Fields, "metadata")
				require.Contains(t, decl.Fields, "spec")

				// kind: string with oneOf / const
				kind := decl.Fields["kind"]
				require.Equal(t, "string", kind.Type.TypeName())
				require.Len(t, kind.EnumValues(), 2) // Example/v1, Example

				// metadata: object
				metadata := decl.Fields["metadata"].Type
				require.True(t, metadata.IsObject())
				// require.ElementsMatch(t, []string{"name"}, metadata.Required())
				require.Contains(t, metadata.Fields, "name")
				require.Contains(t, metadata.Fields, "labels")

				// metadata.labels: map<string,string>, maxLength 20
				labels := metadata.Fields["labels"].Type
				require.True(t, labels.IsMap())
				require.Equal(t, "string", labels.ElemType.TypeName())
				// maxLength is preserved in DeclType metadata
				// require.Equal(t, "20", labels.ElemType.Metadata["maxLength"])

				// spec: object
				spec := decl.Fields["spec"].Type
				require.True(t, spec.IsObject())
				require.Contains(t, spec.Fields, "mode")
				require.Contains(t, spec.Fields, "options")
				require.Contains(t, spec.Fields, "schedule")

				// spec.mode: enum
				mode := spec.Fields["mode"]
				require.Equal(t, "string", mode.Type.TypeName())
				require.Len(t, mode.EnumValues(), 2) // fast, slow

				// spec.options: list of Option
				options := spec.Fields["options"].Type
				require.True(t, options.IsList())
				// require.Equal(t, "test/ComplexMixed.schema.json#/Option", options.ElemType.TypeName())
			},
			exprTests: []struct {
				expr  string
				parse IssueAssertionFunc
				check IssueAssertionFunc
			}{
				// kind ------------------------------------------------
				{"instance.kind", NoIssues, NoIssues},
				{"instance.kind == \"Example/v1\"", NoIssues, NoIssues},
				{"instance.kind == \"Example\"", NoIssues, NoIssues},
				{"instance.kind == 123", NoIssues, IssuesExist},

				// metadata -------------------------------------------
				{"instance.metadata", NoIssues, NoIssues},
				{"instance.metadata.name", NoIssues, NoIssues},
				{"instance.metadata.labels", NoIssues, NoIssues},
				{"instance.metadata.labels.foo", NoIssues, NoIssues},
				{"instance.metadata.labels.foo == \"bar\"", NoIssues, NoIssues},
				{"instance.metadata.labels.foo == 123", NoIssues, IssuesExist},

				// spec.mode ------------------------------------------
				{"instance.spec", NoIssues, NoIssues},
				{"instance.spec.mode", NoIssues, NoIssues},
				{"instance.spec.mode == \"fast\"", NoIssues, NoIssues},
				{"instance.spec.mode == \"slow\"", NoIssues, NoIssues},
				{"instance.spec.mode == \"other\"", NoIssues, NoIssues /* allowed: enum not enforced */},
				{"instance.spec.mode == 123", NoIssues, IssuesExist},

				// spec.schedule (duration) ---------------------------
				{"instance.spec.schedule", NoIssues, NoIssues},
				{"instance.spec.schedule == \"1h\"", NoIssues, NoIssues},
				{"instance.spec.schedule == 1", NoIssues, IssuesExist},

				// spec.options: list ---------------------------------
				{"instance.spec.options", NoIssues, NoIssues},
				{"instance.spec.options[0]", NoIssues, NoIssues},
				{"instance.spec.options[0].flag", NoIssues, NoIssues},
				{"instance.spec.options[0].threshold", NoIssues, NoIssues},
				{"instance.spec.options[0].threshold == 3", NoIssues, NoIssues},
				{"instance.spec.options[0].threshold == \"foo\"", NoIssues, IssuesExist},

				// invalid field --------------------------------------
				{"instance.unknownField", NoIssues, IssuesExist},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			decl := LoadDeclType(t, root, tt.file)

			t.Run("type verification", func(t *testing.T) {
				tt.verify(t, decl)
			})

			t.Run("expression compatibility", func(t *testing.T) {
				for _, et := range tt.exprTests {
					t.Run(et.expr, func(t *testing.T) {
						env := NewCelEnv(t, decl)
						AssertExpression(t, env, et.expr, et.parse, et.check)
					})
				}
			})
		})
	}
}

// LoadAndCompileSchema loads a JSON schema file from root and compiles it.
func LoadAndCompileSchema(t *testing.T, root *os.Root, path string) *stjsonschemav6.Schema {
	raw, err := root.ReadFile(path)
	require.NoError(t, err)

	unmarshalled, err := stjsonschemav6.UnmarshalJSON(bytes.NewReader(raw))
	require.NoError(t, err)

	compiler := stjsonschemav6.NewCompiler()
	require.NoError(t, compiler.AddResource(path, unmarshalled))

	schema, err := compiler.Compile(path)
	require.NoError(t, err)
	return schema
}

// LoadDeclType loads, compiles, and wraps the schema into a DeclType.
func LoadDeclType(t *testing.T, root *os.Root, path string) *jsonschema.DeclType {
	schema := LoadAndCompileSchema(t, root, path)
	declType := jsonschema.NewSchemaDeclType(schema)
	require.NotNil(t, declType)
	return declType
}

// NewCelEnv returns a CEL environment for a given DeclType.
func NewCelEnv(t *testing.T, decl *jsonschema.DeclType) *cel.Env {
	p := provider.New(decl.Type)
	env, err := cel.NewEnv(
		cel.CustomTypeProvider(p),
		cel.Variable("instance", decl.CelType()),
	)
	require.NoError(t, err)
	return env
}

// AssertExpression runs parse & check assertions in one step.
func AssertExpression(
	t *testing.T,
	env *cel.Env,
	expr string,
	parse IssueAssertionFunc,
	check IssueAssertionFunc,
) {
	ast, iss := env.Parse(expr)
	parse(t, iss, "parse result mismatch")
	if iss != nil {
		return
	}
	_, iss = env.Check(ast)
	check(t, iss, "type-check result mismatch")
}

var NoIssues IssueAssertionFunc = func(t require.TestingT, issues *cel.Issues, _ ...interface{}) {
	require.Nil(t, issues, issues.String())
}

var IssuesExist IssueAssertionFunc = func(t require.TestingT, iss *cel.Issues, _ ...interface{}) {
	require.Error(t, iss.Err())
}

type IssueAssertionFunc func(require.TestingT, *cel.Issues, ...interface{})
