package runtime

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestType struct {
	Type  Type   `json:"type"`
	Value string `json:"value"`
}

func (t *TestType) GetType() Type {
	return t.Type
}

func (t *TestType) SetType(typ Type) {
	t.Type = typ
}

func (t *TestType) DeepCopyTyped() Typed {
	return &TestType{
		Type:  t.Type,
		Value: t.Value,
	}
}

func TestRegistry_Decode_With_Type_Mismatch_Defaulting_On_Raw(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	raw := &Raw{Type: typ, Data: []byte(`{"type": "test.type", "value": "foo"}`)}

	parsed := &TestType{}
	r.NoError(registry.Convert(raw, parsed))
	r.Equal(parsed.Value, "foo")

	r.NoError(registry.Convert(&TestType{Type: typ, Value: "bar"}, parsed))
	r.Equal(parsed.Value, "bar")

	parsed2, err := registry.NewObject(typ)
	r.NoError(err)

	r.NoError(registry.Clone().Convert(raw, parsed2))
	r.IsType(&TestType{}, parsed2)
	r.Equal(parsed2.(*TestType).Value, "foo")

	parsed3, err := registry.Clone().NewObject(typ)
	// forcefully empty the type, because new object defaults to the correct type
	r.NoError(err)
	parsed3.SetType(NewUnversionedType(""))
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), parsed3))
	r.Equal(parsed3.(*TestType).Value, "foo")

	unknown := NewScheme()
	_, err = unknown.NewObject(typ)
	r.Error(err)

	r.Error(unknown.Decode(bytes.NewReader(raw.Data), parsed3))
	newRaw := &Raw{}
	r.Error(unknown.Decode(bytes.NewReader(raw.Data), newRaw))

	unknown = NewScheme(WithAllowUnknown())
	parsed4, err := unknown.NewObject(typ)
	r.NoError(err)
	r.NoError(unknown.Decode(bytes.NewReader(raw.Data), parsed4))
	r.IsType(&Raw{}, parsed4)
	// Version is not set because it is not part of the raw data
	r.Equal(parsed4.(*Raw).Type.String(), "test.type")
}

func TestRegistry_Convert_WithAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme(WithAllowUnknown())
	raw := &Raw{Type: typ, Data: []byte(`{"type": "test/v1", "value": "foo"}`)}

	// Test Raw → Typed conversion
	parsed := &TestType{}
	r.NoError(registry.Convert(raw, parsed))
	r.Equal(parsed.Value, "foo")
	r.Equal(parsed.Type, typ)

	// Test Typed → Raw conversion
	from := &TestType{Type: typ, Value: "bar"}
	target := &Raw{}
	r.NoError(registry.Convert(from, target))
	r.Equal(target.Type, typ)
	r.JSONEq(`{"type": "test/v1", "value": "bar"}`, string(target.Data))

	// Test Raw → Raw conversion
	target2 := &Raw{}
	r.NoError(registry.Convert(raw, target2))
	r.Equal(raw.Type, target2.Type)
	r.Equal(raw.Data, target2.Data)

	// Test Typed → Typed conversion
	to := &TestType{}
	r.NoError(registry.Convert(from, to))
	r.Equal(from.Value, to.Value)
	r.Equal(from.Type, to.Type)
}

func TestRegistry_Decode_UnknownType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme(WithAllowUnknown())
	raw := &Raw{Type: typ, Data: []byte(`{"type": "unknown/v1", "value": "foo"}`)}

	// Test decoding into a Raw object
	parsed := &Raw{}
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), parsed))
	r.Equal(parsed.Type, typ)
	r.JSONEq(string(raw.Data), string(parsed.Data))

	// Test decoding into a typed object with unknown type
	typed := &TestType{}
	r.NoError(registry.Decode(bytes.NewReader(raw.Data), typed))
	r.Equal(typed.Type, typ)
	r.Equal(typed.Value, "foo")
}

func TestRegistry_Decode_UnknownType_WithoutAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme() // Without WithAllowUnknown()
	raw := &Raw{Type: typ, Data: []byte(`{"type": "unknown/v1", "value": "foo"}`)}

	// Test decoding into a Raw object
	parsed := &Raw{}
	r.Error(registry.Decode(bytes.NewReader(raw.Data), parsed))

	// Test decoding into a typed object with unknown type
	typed := &TestType{}
	r.Error(registry.Decode(bytes.NewReader(raw.Data), typed))
}

func TestRegistry_Decode_ErrorCases(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test empty input data
	parsed := &TestType{}
	err := registry.Decode(bytes.NewReader([]byte{}), parsed)
	r.Error(err)
	r.Contains(err.Error(), "cannot decode empty input data")

	// Test invalid JSON input
	invalidJSON := []byte(`{"type": "test/v1", "value": "foo"`) // Missing closing brace
	err = registry.Decode(bytes.NewReader(invalidJSON), parsed)
	r.Error(err)
	r.Contains(err.Error(), "failed to unmarshal raw")

	// Test invalid YAML input
	invalidYAML := []byte("type: test/v1\nvalue: foo\n  extra: invalid") // Invalid indentation
	err = registry.Decode(bytes.NewReader(invalidYAML), parsed)
	r.Error(err)
	r.Contains(err.Error(), "failed to unmarshal raw")

	// Test type mismatch after decoding
	validData := []byte(`{"type": "test/v2", "value": "foo"}`) // Different version than registered
	parsed.SetType(typ)                                        // Set initial type
	err = registry.Decode(bytes.NewReader(validData), parsed)
	r.Error(err)
	r.Contains(err.Error(), "expected type")
}

func TestRegistry_Default_UnknownType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme(WithAllowUnknown())

	// Test defaulting a Raw object
	raw := &Raw{Type: typ}
	updated, err := registry.DefaultType(raw)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already set

	// Test defaulting a typed object with unknown type
	typed := &TestType{Type: typ}
	updated, err = registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already set
}

func TestRegistry_Default_UnknownType_WithoutAllowUnknown(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("unknown", "v1")
	registry := NewScheme() // Without WithAllowUnknown()

	// Test defaulting a Raw object
	raw := &Raw{Type: typ}
	_, err := registry.DefaultType(raw)
	r.Error(err)

	// Test defaulting a typed object with unknown type
	typed := &TestType{Type: typ}
	_, err = registry.DefaultType(typed)
	r.Error(err)
}

func TestRegistry_Default_EmptyType(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test defaulting a typed object with empty type
	typed := &TestType{}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should be updated since it was empty
	r.Equal(typed.Type, typ)
}

func TestRegistry_Default_AlreadySet(t *testing.T) {
	r := require.New(t)
	typ := NewVersionedType("test", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ)

	// Test defaulting a typed object with correct type already set
	typed := &TestType{Type: typ}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it was already correct
	r.Equal(typed.Type, typ)
}

func TestRegistry_Default_DifferentRegisteredType(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	// Test defaulting a typed object with different registered type
	typed := &TestType{Type: typ2}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.False(updated) // Type should not be updated since it's already a valid registered type
	r.Equal(typed.Type, typ2)
}

func TestRegistry_Default_MultipleRegisteredTypesWithoutTypeSet(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	// Test defaulting a typed object with different registered type
	typed := &TestType{}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should not be updated since it's already a valid registered type
	r.Equal(typed.Type, typ1)
}

func TestRegistry_Default_UnregisteredType(t *testing.T) {
	r := require.New(t)
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, typ1)

	// Test defaulting a typed object with unregistered type gets overwritten with the registered type
	typed := &TestType{Type: typ2}
	updated, err := registry.DefaultType(typed)
	r.NoError(err)
	r.True(updated) // Type should be updated since it was unregistered
}

func TestRegistry_Default_NoAlias(t *testing.T) {
	r := require.New(t)
	registry := NewScheme()
	r.Error(registry.RegisterWithAlias(&TestType{}))

	registry.MustRegisterWithAlias(&TestType{}, NewVersionedType("test", "v1"))
	r.Error(registry.RegisterWithAlias(&TestType{}))
}

func TestRegistry_MultipleTypes_With_Alias(t *testing.T) {
	r := require.New(t)
	def := NewVersionedType("test1", "v1")
	alias := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, def, alias)
	err := registry.RegisterWithAlias(&TestType{}, def)
	r.ErrorContains(err, "already registered: as default")
	r.True(IsTypeAlreadyRegisteredError(err))

	err = registry.RegisterWithAlias(&TestType{}, alias)
	r.ErrorContains(err, "already registered: as alias")
	r.True(IsTypeAlreadyRegisteredError(err))
}

func TestRegistry_NewObject_Based_On_Alias(t *testing.T) {
	r := require.New(t)
	def := NewVersionedType("test1", "v1")
	alias := NewVersionedType("test2", "v1")
	registry := NewScheme()
	registry.MustRegisterWithAlias(&TestType{}, def, alias)

	obj, err := registry.NewObject(def)
	r.NoError(err)
	r.Equal(obj.GetType(), def)
	obj, err = registry.NewObject(alias)
	r.NoError(err)
	r.Equal(obj.GetType(), alias)
}

func TestRegistry_RegisterScheme(t *testing.T) {
	// Create source scheme with some types
	sourceScheme := NewScheme()
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	sourceScheme.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	t.Run("successful registration", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		err := targetScheme.RegisterScheme(sourceScheme)
		r.NoError(err)

		// Verify types were registered
		r.True(targetScheme.IsRegistered(typ1))
		r.True(targetScheme.IsRegistered(typ2))

		// Verify we can create new objects
		obj1, err := targetScheme.NewObject(typ1)
		r.NoError(err)
		r.IsType(&TestType{}, obj1)

		obj2, err := targetScheme.NewObject(typ2)
		r.NoError(err)
		r.IsType(&TestType{}, obj2)
	})

	t.Run("nil scheme", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		err := targetScheme.RegisterScheme(nil)
		r.NoError(err)
		// Registering nil scheme should not change the target scheme
		r.Equal(targetScheme.defaults.Len(), 0)
		r.Len(targetScheme.aliases, 0)
	})

	t.Run("duplicate registration", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		// First registration should succeed
		err := targetScheme.RegisterScheme(sourceScheme)
		r.NoError(err)

		// Second registration should fail
		err = targetScheme.RegisterScheme(sourceScheme)
		r.Error(err)
		r.Contains(err.Error(), "already registered")
	})

	t.Run("alias conflict", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		// Register a type with an alias that will conflict
		conflictType := NewVersionedType("conflict", "v1")
		targetScheme.MustRegisterWithAlias(&TestType{}, conflictType, typ2)

		// Try to register the source scheme which has typ2 as an alias
		err := targetScheme.RegisterScheme(sourceScheme)
		r.Error(err)
		r.Contains(err.Error(), "already registered")
	})
}

func TestRegistry_RegisterSchemeType(t *testing.T) {
	// Create source scheme with a type and its alias
	sourceScheme := NewScheme()
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	sourceScheme.MustRegisterWithAlias(&TestType{}, typ1, typ2)

	t.Run("successful registration", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		err := targetScheme.RegisterSchemeType(sourceScheme, typ1)
		r.NoError(err)

		// Verify type was registered
		r.True(targetScheme.IsRegistered(typ1))

		// Verify we can create new object
		obj, err := targetScheme.NewObject(typ1)
		r.NoError(err)
		r.IsType(&TestType{}, obj)

		// Verify alias was also registered
		r.True(targetScheme.IsRegistered(typ2))
		obj2, err := targetScheme.NewObject(typ2)
		r.NoError(err)
		r.IsType(&TestType{}, obj2)
	})

	t.Run("nil scheme", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		err := targetScheme.RegisterSchemeType(nil, typ1)
		r.Error(err)
		r.Contains(err.Error(), "cannot register type from nil scheme")
	})

	t.Run("type not found in source scheme", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		unknownType := NewVersionedType("unknown", "v1")
		err := targetScheme.RegisterSchemeType(sourceScheme, unknownType)
		r.Error(err)
		r.Contains(err.Error(), "type \"unknown/v1\" is not registered in the scheme")
	})

	t.Run("duplicate registration", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		// First registration should succeed
		err := targetScheme.RegisterSchemeType(sourceScheme, typ1)
		r.NoError(err)

		// Second registration should fail
		err = targetScheme.RegisterSchemeType(sourceScheme, typ1)
		r.Error(err)
		r.Contains(err.Error(), "already registered")
	})

	t.Run("alias conflict", func(t *testing.T) {
		r := require.New(t)
		targetScheme := NewScheme()
		// Register a type with an alias that will conflict
		conflictType := NewVersionedType("conflict", "v1")
		targetScheme.MustRegisterWithAlias(&TestType{}, conflictType, typ2)

		// Try to register typ1 which has typ2 as an alias
		err := targetScheme.RegisterSchemeType(sourceScheme, typ1)
		r.Error(err)
		r.Contains(err.Error(), "already registered")
	})
}

func TestScheme_GetTypes(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*Scheme)
		expected map[Type][]Type
	}{
		{
			name: "empty scheme",
			setup: func(s *Scheme) {
				// No setup needed
			},
			expected: map[Type][]Type{},
		},
		{
			name: "single type with no aliases",
			setup: func(s *Scheme) {
				s.MustRegisterWithAlias(&Raw{}, NewVersionedType("Foobar", "v1"))
			},
			expected: map[Type][]Type{
				NewVersionedType("Foobar", "v1"): nil,
			},
		},
		{
			name: "single type with multiple aliases",
			setup: func(s *Scheme) {
				s.MustRegisterWithAlias(
					&Raw{},
					NewVersionedType("Foobar", "v1"),
					NewVersionedType("Foobar", "v1alpha1"),
					NewVersionedType("Foobar", "v1beta1"),
				)
			},
			expected: map[Type][]Type{
				NewVersionedType("Foobar", "v1"): {
					NewVersionedType("Foobar", "v1alpha1"),
					NewVersionedType("Foobar", "v1beta1"),
				},
			},
		},
		{
			name: "multiple types with and without aliases",
			setup: func(s *Scheme) {
				// Type 1 with aliases
				s.MustRegisterWithAlias(
					&Raw{},
					NewVersionedType("Foobar", "v1"),
					NewVersionedType("Foobar", "v1alpha1"),
				)
				// Type 2 without aliases
				s.MustRegisterWithAlias(
					&Raw{},
					NewVersionedType("Config", "v1"),
				)
			},
			expected: map[Type][]Type{
				NewVersionedType("Foobar", "v1"): {
					NewVersionedType("Config", "v1"),
					NewVersionedType("Foobar", "v1alpha1"),
				},
			},
		},
		{
			name: "types registered through RegisterScheme",
			setup: func(s *Scheme) {
				// Create a source scheme
				source := NewScheme()
				source.MustRegisterWithAlias(
					&Raw{},
					NewVersionedType("Foobar", "v1"),
					NewVersionedType("Foobar", "v1alpha1"),
				)
				// Register the source scheme
				err := s.RegisterScheme(source)
				if err != nil {
					t.Fatal(err)
				}
			},
			expected: map[Type][]Type{
				NewVersionedType("Foobar", "v1"): {
					NewVersionedType("Foobar", "v1alpha1"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := NewScheme()
			tt.setup(scheme)
			result := scheme.GetTypes()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistry_RegisterSchemes(t *testing.T) {
	// Create test types
	typ1 := NewVersionedType("test1", "v1")
	typ2 := NewVersionedType("test2", "v1")
	typ3 := NewVersionedType("test3", "v1")

	// Create source schemes
	scheme1 := NewScheme()
	scheme1.MustRegisterWithAlias(&TestType{}, typ1)

	scheme2 := NewScheme()
	scheme2.MustRegisterWithAlias(&TestType{}, typ2)

	scheme3 := NewScheme()
	scheme3.MustRegisterWithAlias(&TestType{}, typ3)

	// Test successful registration of multiple schemes
	t.Run("successful registration", func(t *testing.T) {
		r := require.New(t)
		registry := NewScheme()
		err := registry.RegisterSchemes(scheme1, scheme2, scheme3)
		r.NoError(err)

		// Verify all types are registered
		r.True(registry.IsRegistered(typ1))
		r.True(registry.IsRegistered(typ2))
		r.True(registry.IsRegistered(typ3))
	})

	// Test handling nil schemes
	t.Run("nil schemes", func(t *testing.T) {
		r := require.New(t)
		registry := NewScheme()
		err := registry.RegisterSchemes(nil, scheme1, nil, scheme2)
		r.NoError(err)

		// Verify types from non-nil schemes are registered
		r.True(registry.IsRegistered(typ1))
		r.True(registry.IsRegistered(typ2))
	})

	// Test handling conflicts between schemes
	t.Run("conflicting schemes", func(t *testing.T) {
		r := require.New(t)
		registry := NewScheme()
		registry.MustRegisterWithAlias(&TestType{}, typ1) // Register typ1 first

		// Create a scheme with conflicting type
		conflictingScheme := NewScheme()
		conflictingScheme.MustRegisterWithAlias(&TestType{}, typ1)

		// Try to register schemes, including one with conflict
		err := registry.RegisterSchemes(scheme2, conflictingScheme, scheme3)
		r.Error(err)
		r.Contains(err.Error(), "type \"test1/v1\" is already registered")
		r.True(IsTypeAlreadyRegisteredError(err))

		// Verify only the first scheme was registered
		r.True(registry.IsRegistered(typ1))
		r.True(registry.IsRegistered(typ2))
		r.False(registry.IsRegistered(typ3))
	})

	// Test partial registration
	t.Run("partial registration", func(t *testing.T) {
		r := require.New(t)
		registry := NewScheme()
		registry.MustRegisterWithAlias(&TestType{}, typ1) // Register typ1 first

		// Create a scheme with conflicting type
		conflictingScheme := NewScheme()
		conflictingScheme.MustRegisterWithAlias(&TestType{}, typ1)

		// Try to register schemes, including one with conflict
		err := registry.RegisterSchemes(scheme2, conflictingScheme, scheme3)
		r.Error(err)

		// Verify only the first scheme was registered
		r.True(registry.IsRegistered(typ1))
		r.True(registry.IsRegistered(typ2))
		r.False(registry.IsRegistered(typ3))
	})
}
