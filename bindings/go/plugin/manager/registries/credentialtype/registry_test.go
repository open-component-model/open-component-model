package credentialtype

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestNewRegistry(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())
	r.NotNil(reg)
	r.NotNil(reg.GetCredentialTypeScheme())
}

func TestGetCredentialTypeScheme_EmptyOnStart(t *testing.T) {
	reg := NewRegistry(t.Context())
	scheme := reg.GetCredentialTypeScheme()
	require.NotNil(t, scheme)
	require.Empty(t, scheme.GetTypes())
}

func TestRegister(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	reg.Register(dummytype.Scheme)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.ShortType)))
}

func TestRegister_MultipleSchemesAreMerged(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	schemeA := runtime.NewScheme()
	schemeA.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredA", "v1"))

	schemeB := runtime.NewScheme()
	schemeB.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredB", "v1"))

	reg.Register(schemeA)
	reg.Register(schemeB)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredA", "v1")))
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredB", "v1")))
}

func TestRegisterFromPlugin(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	customType := runtime.NewVersionedType("MyCredential", "v1")
	reg.RegisterFromPlugin([]types.Type{
		{Type: customType},
	})

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(customType))
}

func TestRegisterFromPlugin_MultipleTypes(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v2")
	reg.RegisterFromPlugin([]types.Type{
		{Type: typeA},
		{Type: typeB},
	})

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typeA))
	r.True(scheme.IsRegistered(typeB))
}

func TestRegisterFromPlugin_EmptySlice(t *testing.T) {
	reg := NewRegistry(t.Context())
	reg.RegisterFromPlugin([]types.Type{})
	require.NotNil(t, reg.GetCredentialTypeScheme())
}

func TestRegisterFromPlugin_Nil(t *testing.T) {
	reg := NewRegistry(t.Context())
	reg.RegisterFromPlugin(nil)
	require.NotNil(t, reg.GetCredentialTypeScheme())
}

// TestRegisterFromPlugin_MultipleTypesDoNotConflictWithRaw verifies that registering
// several plugin credential types does not cause them to alias each other through
// *runtime.Raw. Each type must resolve independently: IsRegistered, NewObject, and
// Convert must all behave as if each type were the only one registered.
func TestRegisterFromPlugin_MultipleTypesDoNotConflictWithRaw(t *testing.T) {
	typeA := runtime.NewVersionedType("PluginCredA", "v1")
	typeB := runtime.NewVersionedType("PluginCredB", "v1")
	typeC := runtime.NewVersionedType("PluginCredC", "v2")

	reg := NewRegistry(t.Context())
	reg.RegisterFromPlugin([]types.Type{
		{Type: typeA},
		{Type: typeB},
		{Type: typeC},
	})

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
			// The returned object must be typed to the requested type, not to
			// another plugin type that happens to share the same *runtime.Raw prototype.
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

		reg2 := NewRegistry(t.Context())
		reg2.RegisterFromPlugin([]types.Type{
			{Type: aliasedType, Aliases: []runtime.Type{aliasType}},
			{Type: unrelated},
		})

		s := reg2.GetCredentialTypeScheme()
		r.True(s.IsRegistered(aliasedType))
		r.True(s.IsRegistered(aliasType))
		r.True(s.IsRegistered(unrelated))

		// unrelated must still resolve to its own Raw, not to aliasedType's canonical
		obj, err := s.NewObject(unrelated)
		r.NoError(err)
		_, ok := obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(unrelated, obj.(*runtime.Raw).GetType())

		// aliasedType must resolve to its own Raw, not to aliasType's canonical
		obj, err = s.NewObject(aliasedType)
		r.NoError(err)
		raw, ok := obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(aliasedType, raw.GetType())

		// aliasType must resolve to the same Raw as aliasedType, but with its own type identity
		obj, err = s.NewObject(aliasType)
		r.NoError(err)
		_, ok = obj.(*runtime.Raw)
		r.True(ok)
		r.Equal(aliasType, obj.GetType())
	})
}
