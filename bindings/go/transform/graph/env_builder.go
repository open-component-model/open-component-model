package graph

//type EnvBuilder struct {
//	declTypes  []*jsonschema.DeclType
//	envOptions []cel.EnvOption
//}
//
//func NewEnvBuilder(staticEnvironment map[string]interface{}) (*EnvBuilder, error) {
//	schema, err := jsonschema.InferFromGoValue(staticEnvironment)
//	if err != nil {
//		return nil, err
//	}
//	envDeclType := jsonschema.DeclTypeFromInvopop(schema)
//	envDeclType = envDeclType.MaybeAssignTypeName("__type_environment")
//	staticEnvVal := types.DefaultTypeAdapter.NativeToValue(staticEnvironment)
//	staticEnvConstant := cel.Constant("environment", envDeclType.CelType(), staticEnvVal)
//
//	return &EnvBuilder{
//		declTypes:  []*jsonschema.DeclType{envDeclType},
//		envOptions: []cel.EnvOption{staticEnvConstant},
//	}, nil
//}
//
//func (envBuilder *EnvBuilder) RegisterDeclTypes(declTypes ...*jsonschema.DeclType) *EnvBuilder {
//	envBuilder.declTypes = append(envBuilder.declTypes, declTypes...)
//	return envBuilder
//}
//
//func (envBuilder *EnvBuilder) RegisterEnvOption(envOptions ...cel.EnvOption) *EnvBuilder {
//	envBuilder.envOptions = append(envBuilder.envOptions, envOptions...)
//	return envBuilder
//}
//
//func (envBuilder *EnvBuilder) CurrentEnv() (*cel.Env, *jsonschema.DeclTypeProvider, error) {
//	baseEnv, err := cel.NewEnv(
//		cel.OptionalTypes(),
//	)
//	if err != nil {
//		return nil, nil, err
//	}
//	provider := jsonschema.NewDeclTypeProvider(envBuilder.declTypes...)
//	opts, err := provider.EnvOptions(baseEnv.CELTypeProvider())
//	if err != nil {
//		return nil, nil, err
//	}
//	newEnv, err := baseEnv.Extend(append(opts, envBuilder.envOptions...)...)
//	if err != nil {
//		return nil, nil, err
//	}
//	return newEnv, provider, nil
//}
