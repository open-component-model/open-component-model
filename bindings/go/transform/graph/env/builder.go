package env

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/provider"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

type Builder struct {
	declTypes  []*stv6jsonschema.DeclType
	envOptions []cel.EnvOption
}

func NewEnvBuilder(staticEnvironment map[string]interface{}) (*Builder, error) {
	schema, err := stv6jsonschema.InferFromGoValue(staticEnvironment)
	if err != nil {
		return nil, err
	}
	schema.ID = "__type_environment"
	declType := stv6jsonschema.NewSchemaDeclType(schema)
	staticEnvVal := types.DefaultTypeAdapter.NativeToValue(staticEnvironment)
	staticEnvConstant := cel.Constant("environment", declType.CelType(), staticEnvVal)

	return &Builder{
		declTypes:  []*stv6jsonschema.DeclType{declType},
		envOptions: []cel.EnvOption{staticEnvConstant},
	}, nil
}

func (envBuilder *Builder) RegisterDeclTypes(declTypes ...*stv6jsonschema.DeclType) *Builder {
	envBuilder.declTypes = append(envBuilder.declTypes, declTypes...)
	return envBuilder
}

func (envBuilder *Builder) RegisterEnvOption(envOptions ...cel.EnvOption) *Builder {
	envBuilder.envOptions = append(envBuilder.envOptions, envOptions...)
	return envBuilder
}

func (envBuilder *Builder) CurrentEnv() (*cel.Env, *provider.DeclTypeProvider, error) {
	baseEnv, err := cel.NewEnv(
		cel.OptionalTypes(),
	)
	if err != nil {
		return nil, nil, err
	}
	declTypes := make([]*decl.Type, len(envBuilder.declTypes))
	for i, t := range envBuilder.declTypes {
		declTypes[i] = t.Type
	}
	provider := provider.New(declTypes...)
	opts, err := provider.EnvOptions(baseEnv.CELTypeProvider())
	if err != nil {
		return nil, nil, err
	}
	newEnv, err := baseEnv.Extend(append(opts, envBuilder.envOptions...)...)
	if err != nil {
		return nil, nil, err
	}
	return newEnv, provider, nil
}
