package credentialtyperepository_test

// Tests for the custom credential type registration path, moved here from the
// credentialrepository package together with the registration logic itself. External plugins
// declare the types they introduce in a capability spec; the manager extracts them and hands them
// to this registry, while the registry owning that capability runs the plugin.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegisterCustomTypes_MultipleCustomCredentialTypes(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v2")
	r.NoError(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, []types.Type{
		{Type: typeA},
		{Type: typeB},
	}))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typeA))
	r.True(scheme.IsRegistered(typeB))
}

func TestRegisterCustomTypes_NoCustomCredentialTypes(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)
	r.NoError(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, nil))
	r.NotNil(reg.GetCredentialTypeScheme())
}

func TestRegisterCustomTypes_ConflictsBetweenPlugins(t *testing.T) {
	typeA := runtime.NewVersionedType("CredA", "v1")
	aliasA := runtime.NewUnversionedType("CredA")
	typeB := runtime.NewVersionedType("CredB", "v1")

	tests := []struct {
		name   string
		first  []types.Type
		second []types.Type
	}{
		{
			name:   "two plugins register the same canonical type",
			first:  []types.Type{{Type: typeA}},
			second: []types.Type{{Type: typeA}},
		},
		{
			name:   "second plugin's canonical conflicts with first plugin's alias",
			first:  []types.Type{{Type: typeA, Aliases: []runtime.Type{aliasA}}},
			second: []types.Type{{Type: aliasA}},
		},
		{
			name:   "second plugin's alias conflicts with first plugin's canonical",
			first:  []types.Type{{Type: typeA}},
			second: []types.Type{{Type: typeB, Aliases: []runtime.Type{typeA}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := newRegistry(t)
			r.NoError(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, tc.first))
			r.Error(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-b"}, tc.second))
		})
	}
}

// TestRegisterCustomTypes_SamePluginDeclaresTwice covers a plugin binary that lists the same
// custom type in more than one of its capability specs: it declares its own type, so it must not collide with
// itself.
func TestRegisterCustomTypes_SamePluginDeclaresTwice(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")
	alias := runtime.NewUnversionedType("CredA")
	plugin := types.Plugin{ID: "plugin-a"}

	r.NoError(reg.RegisterCustomTypes(plugin, []types.Type{{Type: typ, Aliases: []runtime.Type{alias}}}))
	r.NoError(reg.RegisterCustomTypes(plugin, []types.Type{{Type: typ, Aliases: []runtime.Type{alias}}}))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typ))
	r.True(scheme.IsRegistered(alias))
}

// TestRegisterCustomTypes_SamePluginAddsAliasLater covers a plugin binary whose second capability
// spec adds an alias to a type it already declared: the type itself is known, but the new alias
// still has to be registered.
func TestRegisterCustomTypes_SamePluginAddsAliasLater(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")
	alias := runtime.NewUnversionedType("CredA")
	plugin := types.Plugin{ID: "plugin-a"}

	r.NoError(reg.RegisterCustomTypes(plugin, []types.Type{{Type: typ}}))
	r.NoError(reg.RegisterCustomTypes(plugin, []types.Type{{Type: typ, Aliases: []runtime.Type{alias}}}))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typ))
	r.True(scheme.IsRegistered(alias), "the alias added by the second declaration must be registered")

	obj, err := scheme.NewObject(alias)
	r.NoError(err)
	raw, ok := obj.(*runtime.Raw)
	r.True(ok)
	r.Equal(alias, raw.GetType())
}

// TestRegisterCustomTypes_BuiltinTypeIsNotClaimable guards types registered by a builtin plugin: they have no
// declaring plugin ID, so an external plugin must not be able to take them over.
func TestRegisterCustomTypes_BuiltinTypeIsNotClaimable(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")
	builtin := runtime.NewScheme()
	builtin.MustRegisterWithAlias(&runtime.Raw{}, typ)

	r.NoError(reg.Register(builtin))
	r.ErrorContains(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, []types.Type{{Type: typ}}), "already registered")
}

func TestRegisterCustomTypes_NonConflictingTypesStillRegistered(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v1")

	r.NoError(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, []types.Type{{Type: typeA}}))
	r.Error(reg.RegisterCustomTypes(types.Plugin{ID: "plugin-b"}, []types.Type{
		{Type: typeA},
		{Type: typeB},
	}))
	r.True(reg.GetCredentialTypeScheme().IsRegistered(typeB), "non-conflicting type must still be registered")
}

// TestRegisterCustomTypes_MultipleTypesDoNotConflictWithRaw verifies that registering several plugin
// credential types does not cause them to alias each other through *runtime.Raw.
func TestRegisterCustomTypes_MultipleTypesDoNotConflictWithRaw(t *testing.T) {
	typeA := runtime.NewVersionedType("PluginCredA", "v1")
	typeB := runtime.NewVersionedType("PluginCredB", "v1")
	typeC := runtime.NewVersionedType("PluginCredC", "v2")

	reg := newRegistry(t)
	require.NoError(t, reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, []types.Type{
		{Type: typeA},
		{Type: typeB},
		{Type: typeC},
	}))

	scheme := reg.GetCredentialTypeScheme()

	t.Run("all types are registered", func(t *testing.T) {
		r := require.New(t)
		r.True(scheme.IsRegistered(typeA))
		r.True(scheme.IsRegistered(typeB))
		r.True(scheme.IsRegistered(typeC))
	})

	t.Run("NewObject returns a fresh Raw typed to exactly the requested type", func(t *testing.T) {
		r := require.New(t)
		for _, typ := range []runtime.Type{typeA, typeB, typeC} {
			obj, err := scheme.NewObject(typ)
			r.NoError(err, "NewObject(%s)", typ)
			raw, ok := obj.(*runtime.Raw)
			r.True(ok, "expected *runtime.Raw for %s, got %T", typ, obj)
			r.Equal(typ, raw.GetType(), "NewObject(%s) returned wrong type", typ)
		}
	})

	t.Run("Convert preserves each type's identity", func(t *testing.T) {
		r := require.New(t)
		for _, typ := range []runtime.Type{typeA, typeB, typeC} {
			src := &runtime.Raw{Type: typ, Data: []byte(`{"type":"` + typ.String() + `","value":"x"}`)}

			into, err := scheme.NewObject(typ)
			r.NoError(err)
			r.NoError(scheme.Convert(src, into))

			result, ok := into.(*runtime.Raw)
			r.True(ok)
			r.Equal(typ, result.GetType(), "Convert for %s must not bleed into another type", typ)
		}
	})

	t.Run("aliases within a type do not affect other types", func(t *testing.T) {
		r := require.New(t)
		aliasedType := runtime.NewVersionedType("PluginCredWithAlias", "v1")
		aliasType := runtime.NewUnversionedType("PluginCredWithAlias")
		unrelated := runtime.NewVersionedType("Unrelated", "v1")

		reg2 := newRegistry(t)
		r.NoError(reg2.RegisterCustomTypes(types.Plugin{ID: "plugin-b"}, []types.Type{
			{Type: aliasedType, Aliases: []runtime.Type{aliasType}},
			types.Type{Type: unrelated},
		}))

		s := reg2.GetCredentialTypeScheme()
		r.True(s.IsRegistered(aliasedType))
		r.True(s.IsRegistered(aliasType))
		r.True(s.IsRegistered(unrelated))

		obj, err := s.NewObject(unrelated)
		r.NoError(err)
		raw, ok := obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(unrelated, raw.GetType())

		obj, err = s.NewObject(aliasedType)
		r.NoError(err)
		raw, ok = obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(aliasedType, raw.GetType())

		obj, err = s.NewObject(aliasType)
		r.NoError(err)
		raw, ok = obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(aliasType, raw.GetType())
	})
}

func TestRegisterCustomTypes_ConvertRoundTrips(t *testing.T) {
	pluginType := runtime.NewVersionedType("PluginTokenA", "v1")
	reg := newRegistry(t)
	require.NoError(t, reg.RegisterCustomTypes(types.Plugin{ID: "plugin-a"}, []types.Type{
		{Type: pluginType},
	}))

	scheme := reg.GetCredentialTypeScheme()

	t.Run("Convert round-trips plugin credentials as *runtime.Raw", func(t *testing.T) {
		r := require.New(t)
		raw := &runtime.Raw{
			Type: pluginType,
			Data: []byte(`{"type":"PluginTokenA/v1","token":"secret-value"}`),
		}

		into, err := scheme.NewObject(pluginType)
		r.NoError(err)
		r.NoError(scheme.Convert(raw, into))

		result, ok := into.(*runtime.Raw)
		r.True(ok)
		r.Equal(pluginType, result.GetType())
	})

	t.Run("NewObject returns *runtime.Raw for plugin types", func(t *testing.T) {
		r := require.New(t)
		obj, err := scheme.NewObject(pluginType)
		r.NoError(err)
		_, ok := obj.(*runtime.Raw)
		r.True(ok, "expected *runtime.Raw for plugin type, got %T", obj)
	})
}
