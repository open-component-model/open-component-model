package check

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/provider"
)

func TestPrimitiveTypes(t *testing.T) {
	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "string to string",
			output:     cel.StringType,
			expected:   cel.StringType,
			compatible: true,
		},
		{
			name:       "int to int",
			output:     cel.IntType,
			expected:   cel.IntType,
			compatible: true,
		},
		{
			name:       "bool to bool",
			output:     cel.BoolType,
			expected:   cel.BoolType,
			compatible: true,
		},
		{
			name:       "double to double",
			output:     cel.DoubleType,
			expected:   cel.DoubleType,
			compatible: true,
		},
		{
			name:        "string to int",
			output:      cel.StringType,
			expected:    cel.IntType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:        "int to string",
			output:      cel.IntType,
			expected:    cel.StringType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:        "bool to string",
			output:      cel.BoolType,
			expected:    cel.StringType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:       "dyn to string",
			output:     cel.DynType,
			expected:   cel.StringType,
			compatible: true,
		},
		{
			name:       "string to dyn",
			output:     cel.StringType,
			expected:   cel.DynType,
			compatible: true,
		},
		{
			name:        "nil output type",
			output:      nil,
			expected:    cel.StringType,
			compatible:  false,
			errContains: "nil type",
		},
		{
			name:        "nil expected type",
			output:      cel.StringType,
			expected:    nil,
			compatible:  false,
			errContains: "nil type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestListTypes(t *testing.T) {
	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "[]string to []string",
			output:     cel.ListType(cel.StringType),
			expected:   cel.ListType(cel.StringType),
			compatible: true,
		},
		{
			name:       "[]int to []int",
			output:     cel.ListType(cel.IntType),
			expected:   cel.ListType(cel.IntType),
			compatible: true,
		},
		{
			name:        "[]string to []int",
			output:      cel.ListType(cel.StringType),
			expected:    cel.ListType(cel.IntType),
			compatible:  false,
			errContains: "list element type incompatible",
		},
		{
			name:        "[]string to string",
			output:      cel.ListType(cel.StringType),
			expected:    cel.StringType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:       "[]dyn to []string",
			output:     cel.ListType(cel.DynType),
			expected:   cel.ListType(cel.StringType),
			compatible: true,
		},
		{
			name:       "[][]string to [][]string",
			output:     cel.ListType(cel.ListType(cel.StringType)),
			expected:   cel.ListType(cel.ListType(cel.StringType)),
			compatible: true,
		},
		{
			name:        "[][]string to [][]int",
			output:      cel.ListType(cel.ListType(cel.StringType)),
			expected:    cel.ListType(cel.ListType(cel.IntType)),
			compatible:  false,
			errContains: "list element type incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestMapTypes(t *testing.T) {
	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "map[string]string to map[string]string",
			output:     cel.MapType(cel.StringType, cel.StringType),
			expected:   cel.MapType(cel.StringType, cel.StringType),
			compatible: true,
		},
		{
			name:       "map[string]int to map[string]int",
			output:     cel.MapType(cel.StringType, cel.IntType),
			expected:   cel.MapType(cel.StringType, cel.IntType),
			compatible: true,
		},
		{
			name:        "map[string]string to map[string]int",
			output:      cel.MapType(cel.StringType, cel.StringType),
			expected:    cel.MapType(cel.StringType, cel.IntType),
			compatible:  false,
			errContains: "map value type incompatible",
		},
		{
			name:        "map[string]int to map[int]int",
			output:      cel.MapType(cel.StringType, cel.IntType),
			expected:    cel.MapType(cel.IntType, cel.IntType),
			compatible:  false,
			errContains: "map key type incompatible",
		},
		{
			name:        "map[string]string to string",
			output:      cel.MapType(cel.StringType, cel.StringType),
			expected:    cel.StringType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:       "map[string]dyn to map[string]string",
			output:     cel.MapType(cel.StringType, cel.DynType),
			expected:   cel.MapType(cel.StringType, cel.StringType),
			compatible: true,
		},
		{
			name:       "map[string]map[string]int to map[string]map[string]int",
			output:     cel.MapType(cel.StringType, cel.MapType(cel.StringType, cel.IntType)),
			expected:   cel.MapType(cel.StringType, cel.MapType(cel.StringType, cel.IntType)),
			compatible: true,
		},
		{
			name:        "map[string]map[string]int to map[string]map[string]string",
			output:      cel.MapType(cel.StringType, cel.MapType(cel.StringType, cel.IntType)),
			expected:    cel.MapType(cel.StringType, cel.MapType(cel.StringType, cel.StringType)),
			compatible:  false,
			errContains: "map value type incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestStructTypes(t *testing.T) {
	personFields := map[string]*decl.Field{
		"name":  decl.NewField("name", decl.StringType, true, nil, nil),
		"age":   decl.NewField("age", decl.IntType, false, nil, nil),
		"email": decl.NewField("email", decl.StringType, false, nil, nil),
	}
	personType := decl.NewObjectType(TypeNamePrefix+"person", personFields)

	personSubsetFields := map[string]*decl.Field{
		"name": decl.NewField("name", decl.StringType, true, nil, nil),
		"age":  decl.NewField("age", decl.IntType, false, nil, nil),
	}
	personSubsetType := decl.NewObjectType(TypeNamePrefix+"personSubset", personSubsetFields)

	personWithExtraFields := map[string]*decl.Field{
		"name":       decl.NewField("name", decl.StringType, true, nil, nil),
		"age":        decl.NewField("age", decl.IntType, false, nil, nil),
		"email":      decl.NewField("email", decl.StringType, false, nil, nil),
		"extraField": decl.NewField("extraField", decl.StringType, false, nil, nil),
	}
	personWithExtraType := decl.NewObjectType(TypeNamePrefix+"personWithExtra", personWithExtraFields)

	personWrongTypeFields := map[string]*decl.Field{
		"name": decl.NewField("name", decl.StringType, true, nil, nil),
		"age":  decl.NewField("age", decl.StringType, false, nil, nil),
	}
	personWrongTypeType := decl.NewObjectType(TypeNamePrefix+"personWrongType", personWrongTypeFields)

	provider := provider.New(personType, personSubsetType, personWithExtraType, personWrongTypeType)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "identical structs",
			output:     personType.CelType(),
			expected:   personType.CelType(),
			compatible: true,
		},
		{
			name:       "subset struct",
			output:     personSubsetType.CelType(),
			expected:   personType.CelType(),
			compatible: true,
		},
		{
			name:        "struct with extra field",
			output:      personWithExtraType.CelType(),
			expected:    personType.CelType(),
			compatible:  false,
			errContains: "exists in output but not in expected type",
		},
		{
			name:        "struct with wrong field type",
			output:      personWrongTypeType.CelType(),
			expected:    personType.CelType(),
			compatible:  false,
			errContains: "has incompatible type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, provider)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestNestedTypes(t *testing.T) {
	addressFields := map[string]*decl.Field{
		"street": decl.NewField("street", decl.StringType, true, nil, nil),
		"city":   decl.NewField("city", decl.StringType, true, nil, nil),
		"zip":    decl.NewField("zip", decl.StringType, false, nil, nil),
	}
	addressType := decl.NewObjectType(TypeNamePrefix+"address", addressFields)

	userFields := map[string]*decl.Field{
		"name":    decl.NewField("name", decl.StringType, true, nil, nil),
		"address": decl.NewField("address", addressType, false, nil, nil),
		"tags":    decl.NewField("tags", decl.NewListType(decl.StringType, decl.NoMaxLength), false, nil, nil),
	}
	userType := decl.NewObjectType(TypeNamePrefix+"user", userFields)

	addressSubsetFields := map[string]*decl.Field{
		"street": decl.NewField("street", decl.StringType, true, nil, nil),
		"city":   decl.NewField("city", decl.StringType, true, nil, nil),
	}
	addressSubsetType := decl.NewObjectType(TypeNamePrefix+"addressSubset", addressSubsetFields)

	userSubsetFields := map[string]*decl.Field{
		"name":    decl.NewField("name", decl.StringType, true, nil, nil),
		"address": decl.NewField("address", addressSubsetType, false, nil, nil),
	}
	userSubsetType := decl.NewObjectType(TypeNamePrefix+"userSubset", userSubsetFields)

	addressWrongTypeFields := map[string]*decl.Field{
		"street": decl.NewField("street", decl.IntType, true, nil, nil),
		"city":   decl.NewField("city", decl.StringType, true, nil, nil),
	}
	addressWrongType := decl.NewObjectType(TypeNamePrefix+"addressWrongType", addressWrongTypeFields)

	userWrongTypeFields := map[string]*decl.Field{
		"name":    decl.NewField("name", decl.StringType, true, nil, nil),
		"address": decl.NewField("address", addressWrongType, false, nil, nil),
	}
	userWrongType := decl.NewObjectType(TypeNamePrefix+"userWrongType", userWrongTypeFields)

	provider := provider.New(
		addressType, addressSubsetType, addressWrongType,
		userType, userSubsetType, userWrongType,
	)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "identical nested structs",
			output:     userType.CelType(),
			expected:   userType.CelType(),
			compatible: true,
		},
		{
			name:       "nested struct subset",
			output:     userSubsetType.CelType(),
			expected:   userType.CelType(),
			compatible: true,
		},
		{
			name:        "nested struct wrong type",
			output:      userWrongType.CelType(),
			expected:    userType.CelType(),
			compatible:  false,
			errContains: "has incompatible type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, provider)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestMapToStructCompatibility(t *testing.T) {
	// homogeneous struct: {a:int, b:int}
	intStructFields := map[string]*decl.Field{
		"a": decl.NewField("a", decl.IntType, true, nil, nil),
		"b": decl.NewField("b", decl.IntType, false, nil, nil),
	}
	intStruct := decl.NewObjectType(TypeNamePrefix+"intStruct", intStructFields)

	// homogeneous struct: {x:string, y:string}
	stringStructFields := map[string]*decl.Field{
		"x": decl.NewField("x", decl.StringType, true, nil, nil),
		"y": decl.NewField("y", decl.StringType, true, nil, nil),
	}
	stringStruct := decl.NewObjectType(TypeNamePrefix+"stringStruct", stringStructFields)

	nestedObjectType := decl.NewObjectType(TypeNamePrefix+"nestedObject", map[string]*decl.Field{
		"int": decl.NewField("int", decl.IntType, true, nil, nil),
	})
	parentObjectType := decl.NewObjectType(TypeNamePrefix+"parentObject", map[string]*decl.Field{
		"string": decl.NewField("string", decl.StringType, true, nil, nil),
		"nested": decl.NewField("nested", nestedObjectType, false, nil, nil),
	})

	provider := provider.New(intStruct, stringStruct, parentObjectType, nestedObjectType)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "map[string]int → struct{a:int, b:int} OK",
			output:     cel.MapType(cel.StringType, cel.IntType),
			expected:   intStruct.CelType(),
			compatible: true,
		},
		{
			name:       "map[string]optional<int> → struct{a:int, b:int} OK",
			output:     cel.MapType(cel.StringType, cel.OptionalType(cel.IntType)),
			expected:   intStruct.CelType(),
			compatible: true,
		},
		{
			name:       "map[string]string → struct{x:string, y:string} OK",
			output:     cel.MapType(cel.StringType, cel.StringType),
			expected:   stringStruct.CelType(),
			compatible: true,
		},
		{
			name:       "map[string]dyn → struct{x:string, y:string} OK",
			output:     cel.MapType(cel.StringType, cel.DynType),
			expected:   stringStruct.CelType(),
			compatible: true,
		},
		{
			name:        "map[int]string → struct{x:string,y:string} invalid (key type)",
			output:      cel.MapType(cel.IntType, cel.StringType),
			expected:    stringStruct.CelType(),
			compatible:  false,
			errContains: "keys must be strings",
		},
		{
			name:        "map[string]int → struct{x:string,y:string} incompatible",
			output:      cel.MapType(cel.StringType, cel.IntType),
			expected:    stringStruct.CelType(),
			compatible:  false,
			errContains: "incompatible",
		},
		{
			name:       "map[string]dyn → struct{x:string, y: struct{x: int}} OK",
			output:     cel.MapType(cel.StringType, cel.DynType),
			expected:   parentObjectType.CelType(),
			compatible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, provider)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestStructToMapCompatibility(t *testing.T) {
	intStructFields := map[string]*decl.Field{
		"a": decl.NewField("a", decl.IntType, true, nil, nil),
		"b": decl.NewField("b", decl.IntType, false, nil, nil),
	}
	intStruct := decl.NewObjectType(TypeNamePrefix+"intStruct", intStructFields)

	stringStructFields := map[string]*decl.Field{
		"x": decl.NewField("x", decl.StringType, true, nil, nil),
		"y": decl.NewField("y", decl.StringType, true, nil, nil),
	}
	stringStruct := decl.NewObjectType(TypeNamePrefix+"stringStruct", stringStructFields)

	provider := provider.New(intStruct, stringStruct)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "struct{a:int,b:int} → map[string]int OK",
			output:     intStruct.CelType(),
			expected:   cel.MapType(cel.StringType, cel.IntType),
			compatible: true,
		},
		{
			name:       "struct{x:string,y:string} → map[string]string OK",
			output:     stringStruct.CelType(),
			expected:   cel.MapType(cel.StringType, cel.StringType),
			compatible: true,
		},
		{
			name:       "struct{x:string,y:string} → map[string]dyn OK",
			output:     stringStruct.CelType(),
			expected:   cel.MapType(cel.StringType, cel.DynType),
			compatible: true,
		},
		{
			name:        "struct{x:string,y:string} → map[int]string invalid (bad key)",
			output:      stringStruct.CelType(),
			expected:    cel.MapType(cel.IntType, cel.StringType),
			compatible:  false,
			errContains: "key type must be string",
		},
		{
			name:        "struct{x:string,y:string} → map[string]int incompatible",
			output:      stringStruct.CelType(),
			expected:    cel.MapType(cel.StringType, cel.IntType),
			compatible:  false,
			errContains: "incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, provider)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestOptionalPrimitive(t *testing.T) {
	optionalString := cel.OpaqueType("optional_type", cel.StringType)
	optionalInt := cel.OpaqueType("optional_type", cel.IntType)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "optional<string> to string",
			output:     optionalString,
			expected:   cel.StringType,
			compatible: true,
		},
		{
			name:        "optional<string> to int",
			output:      optionalString,
			expected:    cel.IntType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:        "optional<int> to string",
			output:      optionalInt,
			expected:    cel.StringType,
			compatible:  false,
			errContains: "kind mismatch",
		},
		{
			name:       "optional(optional(string)) to string",
			output:     cel.OpaqueType("optional_type", optionalString),
			expected:   cel.StringType,
			compatible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

func TestOptionalListTypes(t *testing.T) {
	optionalString := cel.OpaqueType("optional_type", cel.StringType)
	optionalListString := cel.OpaqueType("optional_type", cel.ListType(cel.StringType))

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "optional<string> list element → list<string>",
			output:     cel.ListType(optionalString),
			expected:   cel.ListType(cel.StringType),
			compatible: true,
		},
		{
			name:       "optional<list<string>> → list<string>",
			output:     optionalListString,
			expected:   cel.ListType(cel.StringType),
			compatible: true,
		},
		{
			name:        "list<optional<string>> → list<int>",
			output:      cel.ListType(optionalString),
			expected:    cel.ListType(cel.IntType),
			compatible:  false,
			errContains: "list element type incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

func TestOptionalMapTypes(t *testing.T) {
	optionalString := cel.OpaqueType("optional_type", cel.StringType)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "map[string]optional<string> → map[string]string",
			output:     cel.MapType(cel.StringType, optionalString),
			expected:   cel.MapType(cel.StringType, cel.StringType),
			compatible: true,
		},
		{
			name:        "map[string]optional<string> → map[string]int",
			output:      cel.MapType(cel.StringType, optionalString),
			expected:    cel.MapType(cel.StringType, cel.IntType),
			compatible:  false,
			errContains: "map value type incompatible",
		},
		{
			name:       "map[optional<string>]string → map[string]string (key)",
			output:     cel.MapType(optionalString, cel.StringType),
			expected:   cel.MapType(cel.StringType, cel.StringType),
			compatible: true, // optional<string> → string ok
		},
		{
			name:        "map[optional<string>]string → map[int]string",
			output:      cel.MapType(optionalString, cel.StringType),
			expected:    cel.MapType(cel.IntType, cel.StringType),
			compatible:  false,
			errContains: "map key type incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, nil)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

func TestOptionalStructTypes(t *testing.T) {
	fields := map[string]*decl.Field{
		"name": decl.NewField("name", decl.StringType, true, nil, nil),
	}
	personType := decl.NewObjectType(TypeNamePrefix+"person", fields)
	otherType := decl.NewObjectType(TypeNamePrefix+"other", fields)

	unknownFieldsType := decl.NewObjectType(TypeNamePrefix+"unknownFields", map[string]*decl.Field{})
	unknownFieldsType.SetAdditionalPropertiesMetadata(true)

	optionalString := cel.OptionalType(cel.StringType)
	optionalPerson := cel.OptionalType(personType.CelType())

	provider := provider.New(personType, otherType, unknownFieldsType)

	tests := []struct {
		name        string
		output      *cel.Type
		expected    *cel.Type
		compatible  bool
		errContains string
	}{
		{
			name:       "optional<string> field inside struct",
			output:     cel.ListType(optionalString), // simulate a struct field → list<string>
			expected:   cel.ListType(cel.StringType),
			compatible: true,
		},
		{
			name:       "optional<person> → person",
			output:     optionalPerson,
			expected:   personType.CelType(),
			compatible: true,
		},
		{
			name:       "optional<person> → empty struct ok (permissive right now, could be rejected as well)",
			output:     optionalPerson,
			expected:   decl.NewObjectType(TypeNamePrefix+"noFields", nil).CelType(),
			compatible: true,
		},
		{
			name:       "optional<person> → matching struct",
			output:     optionalPerson,
			expected:   otherType.CelType(),
			compatible: true,
		},
		{
			name:       "optional<person> → unknown field allowing type",
			output:     optionalPerson,
			expected:   unknownFieldsType.CelType(),
			compatible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatible, err := AreTypesStructurallyCompatible(tt.output, tt.expected, provider)
			assert.Equal(t, tt.compatible, compatible)
			if tt.compatible {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}
