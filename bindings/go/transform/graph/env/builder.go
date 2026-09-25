package env

import (
	"maps"
	"slices"
	"sync"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/ext"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/provider"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

// Builder constructs CEL environments for transformation graph analysis and
// evaluation. Declaration types are merged incrementally into registeredTypes
// using copy-on-write, so CurrentEnv does not re-walk previously registered
// schemas, and providers and environments created earlier keep their own
// snapshot of the type map.
//
// A Builder is safe for concurrent use: all mutations and reads of the shared
// envOptions and registeredTypes are guarded by mu. Combined with the
// copy-on-write of registeredTypes, environments and providers returned by
// CurrentEnv and Provider keep a stable snapshot that later registrations do
// not disturb.
type Builder struct {
	mu              sync.Mutex
	envOptions      []cel.EnvOption
	registeredTypes map[string]*decl.Type
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

	b := &Builder{envOptions: []cel.EnvOption{staticEnvConstant}}
	return b.RegisterDeclTypes(declType), nil
}

// RegisterDeclTypes merges the given declaration types into the builder's type
// map using copy-on-write: the merge works on a clone of the current map.
// On type name collisions the later registration wins.
func (envBuilder *Builder) RegisterDeclTypes(declTypes ...*stv6jsonschema.DeclType) *Builder {
	envBuilder.mu.Lock()
	defer envBuilder.mu.Unlock()
	merged := maps.Clone(envBuilder.registeredTypes)
	if merged == nil {
		// Support the zero value of Builder: cloning a nil map leaves nil.
		merged = make(map[string]*decl.Type, len(declTypes))
	}
	for _, declType := range declTypes {
		for name, t := range provider.FieldTypeMap(declType.TypeName(), declType.Type) {
			merged[name] = t
		}
	}
	envBuilder.registeredTypes = merged
	return envBuilder
}

func (envBuilder *Builder) RegisterEnvOption(envOptions ...cel.EnvOption) *Builder {
	envBuilder.mu.Lock()
	defer envBuilder.mu.Unlock()
	envBuilder.envOptions = append(envBuilder.envOptions, envOptions...)
	return envBuilder
}

func (envBuilder *Builder) CurrentEnv() (*cel.Env, *provider.DeclTypeProvider, error) {
	baseEnv, err := cel.NewEnv(
		cel.OptionalTypes(),
		ext.Encoders(),
		URL(),
		ContentDigestAlgorithm(),
		Hex(),
	)
	if err != nil {
		return nil, nil, err
	}
	envBuilder.mu.Lock()
	provider := provider.NewFromTypeMap(envBuilder.registeredTypes)
	envOptions := slices.Clone(envBuilder.envOptions)
	envBuilder.mu.Unlock()

	opts, err := provider.EnvOptions(baseEnv.CELTypeProvider())
	if err != nil {
		return nil, nil, err
	}
	newEnv, err := baseEnv.Extend(append(opts, envOptions...)...)
	if err != nil {
		return nil, nil, err
	}
	return newEnv, provider, nil
}

// Provider returns a DeclTypeProvider over all declaration types registered so
// far. The provider holds its own snapshot of the type map: registrations made
// after this call are not visible to it.
func (envBuilder *Builder) Provider() *provider.DeclTypeProvider {
	envBuilder.mu.Lock()
	defer envBuilder.mu.Unlock()
	return provider.NewFromTypeMap(envBuilder.registeredTypes)
}
