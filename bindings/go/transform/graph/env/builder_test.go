package env

import (
	"testing"

	"cel.dev/cel-go/cel"
	"github.com/stretchr/testify/require"

	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

func newSchemaDeclType(t *testing.T, id string, value map[string]interface{}) *stv6jsonschema.DeclType {
	t.Helper()
	r := require.New(t)
	schema, err := stv6jsonschema.InferFromGoValue(value)
	r.NoError(err)
	schema.ID = id
	return stv6jsonschema.NewSchemaDeclType(schema)
}

func TestNewEnvBuilder_CurrentEnvCompilesAgainstEnvironment(t *testing.T) {
	r := require.New(t)

	b, err := NewEnvBuilder(map[string]interface{}{
		"spec": map[string]interface{}{"name": "ocm"},
	})
	r.NoError(err)

	env, _, err := b.CurrentEnv()
	r.NoError(err)

	ast, issues := env.Compile(`environment.spec.name == "ocm"`)
	r.NoError(issues.Err())
	r.NotNil(ast)
}

func TestBuilder_ZeroValueUsable(t *testing.T) {
	r := require.New(t)

	var b Builder
	registered := newSchemaDeclType(t, "late", map[string]interface{}{"x": "y"})
	b.RegisterDeclTypes(registered)

	_, found := b.Provider().FindDeclType(registered.TypeName())
	r.True(found)

	_, _, err := b.CurrentEnv()
	r.NoError(err)
}

func TestRegisterDeclTypes_LaterRegistrationWins(t *testing.T) {
	r := require.New(t)

	b, err := NewEnvBuilder(map[string]interface{}{})
	r.NoError(err)

	first := newSchemaDeclType(t, "shared", map[string]interface{}{"a": "string"})
	b.RegisterDeclTypes(first)
	second := newSchemaDeclType(t, "shared", map[string]interface{}{"b": float64(42)})
	b.RegisterDeclTypes(second)
	r.Equal(first.TypeName(), second.TypeName())

	declType, found := b.Provider().FindDeclType(second.TypeName())
	r.True(found)
	_, hasB := declType.Fields["b"]
	r.True(hasB, "later registration must win on type name collision")
	_, hasA := declType.Fields["a"]
	r.False(hasA, "overwritten registration must not leak fields")
}

func TestRegisterDeclTypes_SnapshotIsolation(t *testing.T) {
	r := require.New(t)

	b, err := NewEnvBuilder(map[string]interface{}{"base": true})
	r.NoError(err)

	typeA := newSchemaDeclType(t, "type_a", map[string]interface{}{"a": "value"})
	b.RegisterDeclTypes(typeA)
	before := b.Provider()

	typeB := newSchemaDeclType(t, "type_b", map[string]interface{}{"b": float64(1)})
	b.RegisterDeclTypes(typeB)

	_, found := before.FindDeclType(typeB.TypeName())
	r.False(found, "provider created before the registration must not see the new type")
	_, found = before.FindDeclType(typeA.TypeName())
	r.True(found, "provider snapshot must keep its own view of earlier types")

	after := b.Provider()
	_, found = after.FindDeclType(typeB.TypeName())
	r.True(found, "a provider created after the registration must see the new type")
	_, found = after.FindDeclType(typeA.TypeName())
	r.True(found)
}

func TestCurrentEnv_CompilesAgainstRegisteredType(t *testing.T) {
	r := require.New(t)

	b, err := NewEnvBuilder(map[string]interface{}{})
	r.NoError(err)

	first := newSchemaDeclType(t, "input", map[string]interface{}{
		"nested": map[string]interface{}{"value": float64(42)},
	})
	b.RegisterDeclTypes(first)
	b.RegisterEnvOption(cel.Variable("input", first.CelType()))

	env, provider, err := b.CurrentEnv()
	r.NoError(err)
	ast, issues := env.Compile("input == input")
	r.NoError(issues.Err())
	r.NotNil(ast)
	fieldType, found := provider.FindStructFieldType(first.TypeName(), "nested")
	r.True(found)
	r.NotNil(fieldType.Type)

	// Registering a second type after the env and provider were created must
	// not disturb them.
	second := newSchemaDeclType(t, "other", map[string]interface{}{"flag": true})
	b.RegisterDeclTypes(second)
	b.RegisterEnvOption(cel.Variable("other", second.CelType()))

	_, issues = env.Compile("input == input")
	r.NoError(issues.Err(), "env created before the second registration must still compile")
	_, found = provider.FindDeclType(second.TypeName())
	r.False(found, "provider snapshot must not observe registrations made after its creation")

	env2, provider2, err := b.CurrentEnv()
	r.NoError(err)
	ast, issues = env2.Compile("other == other")
	r.NoError(issues.Err())
	r.NotNil(ast)
	_, found = provider2.FindDeclType(first.TypeName())
	r.True(found)
	_, found = provider2.FindDeclType(second.TypeName())
	r.True(found)
}
