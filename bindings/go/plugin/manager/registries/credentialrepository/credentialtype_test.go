package credentialrepository_test

// Tests for the custom credential type registration path that lives inside
// RepositoryRegistry.AddPlugin (via registerCustomCredentialTypes).
// These tests were previously in the credentialtype package before the plugin
// support was consolidated here.

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// credentialTypeCapability builds a CapabilitySpec carrying only CustomCredentialTypes,
// which is the part exercised by registerCustomCredentialTypes.
func credentialTypeCapability(customTypes ...types.Type) *v1.CapabilitySpec {
	return &v1.CapabilitySpec{
		Type:                  runtime.NewUnversionedType(string(v1.CredentialRepositoryPluginType)),
		CustomCredentialTypes: customTypes,
	}
}

func newRepoRegistry(t *testing.T) *credentialrepository.RepositoryRegistry {
	t.Helper()
	return credentialrepository.NewCredentialRepositoryRegistry(t.Context())
}

func TestAddPlugin_MultipleCustomCredentialTypes(t *testing.T) {
	r := require.New(t)
	reg := newRepoRegistry(t)

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v2")
	r.NoError(reg.AddPlugin(types.Plugin{}, credentialTypeCapability(
		types.Type{Type: typeA},
		types.Type{Type: typeB},
	)))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typeA))
	r.True(scheme.IsRegistered(typeB))
}

func TestAddPlugin_NoCustomCredentialTypes(t *testing.T) {
	r := require.New(t)
	reg := newRepoRegistry(t)
	r.NoError(reg.AddPlugin(types.Plugin{}, credentialTypeCapability()))
	r.NotNil(reg.GetCredentialTypeScheme())
}

func TestAddPlugin_ConflictsBetweenPlugins(t *testing.T) {
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
			reg := newRepoRegistry(t)
			r.NoError(reg.AddPlugin(types.Plugin{ID: "plugin-a"}, credentialTypeCapability(tc.first...)))
			r.Error(reg.AddPlugin(types.Plugin{ID: "plugin-b"}, credentialTypeCapability(tc.second...)))
		})
	}
}

// TestAddPlugin_MultipleTypesDoNotConflictWithRaw verifies that registering several
// plugin credential types does not cause them to alias each other through *runtime.Raw.
func TestAddPlugin_MultipleTypesDoNotConflictWithRaw(t *testing.T) {
	typeA := runtime.NewVersionedType("PluginCredA", "v1")
	typeB := runtime.NewVersionedType("PluginCredB", "v1")
	typeC := runtime.NewVersionedType("PluginCredC", "v2")

	reg := newRepoRegistry(t)
	require.NoError(t, reg.AddPlugin(types.Plugin{}, credentialTypeCapability(
		types.Type{Type: typeA},
		types.Type{Type: typeB},
		types.Type{Type: typeC},
	)))

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

		reg2 := newRepoRegistry(t)
		r.NoError(reg2.AddPlugin(types.Plugin{}, credentialTypeCapability(
			types.Type{Type: aliasedType, Aliases: []runtime.Type{aliasType}},
			types.Type{Type: unrelated},
		)))

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

func TestAddPlugin_ConvertRoundTrips(t *testing.T) {
	type customCred struct {
		Type  runtime.Type `json:"type"`
		Token string       `json:"token,omitempty"`
	}

	pluginType := runtime.NewVersionedType("PluginTokenA", "v1")
	reg := newRepoRegistry(t)
	require.NoError(t, reg.AddPlugin(types.Plugin{}, credentialTypeCapability(
		types.Type{Type: pluginType},
	)))

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

// ── Register (built-in scheme merging) ───────────────────────────────────────

func TestRegister_BuiltinScheme(t *testing.T) {
	r := require.New(t)
	reg := newRepoRegistry(t)

	reg.Register(dummytype.Scheme)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.ShortType)))
}

func TestRegister_MultipleSchemesAreMerged(t *testing.T) {
	r := require.New(t)
	reg := newRepoRegistry(t)

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
