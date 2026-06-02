package credentialtype_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	helmcredspec "ocm.software/open-component-model/bindings/go/helm/spec/credentials"
	helmcredv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	rsacredspec "ocm.software/open-component-model/bindings/go/rsa/spec/credentials"
	rsacredv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestNewRegistry(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry(t.Context())
	r.NotNil(reg)
	scheme := reg.GetCredentialTypeScheme()
	r.NotNil(scheme)
	r.Empty(scheme.GetTypes())
}

func TestRegister(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry(t.Context())

	reg.Register(dummytype.Scheme)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.ShortType)))
}

func TestRegister_MultipleSchemesAreMerged(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry(t.Context())

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

func TestRegisterFromPlugin_MultipleTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry(t.Context())

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v2")
	r.NoError(reg.RegisterFromPlugin([]types.Type{
		{Type: typeA},
		{Type: typeB},
	}))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(typeA))
	r.True(scheme.IsRegistered(typeB))
}

func TestRegisterFromPlugin_NoTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry(t.Context())
	r.NoError(reg.RegisterFromPlugin(nil))
	r.NoError(reg.RegisterFromPlugin([]types.Type{}))
	r.NotNil(reg.GetCredentialTypeScheme())
}

func TestRegisterFromPlugin_ConflictsBetweenPlugins(t *testing.T) {
	typeA := runtime.NewVersionedType("CredA", "v1")
	aliasA := runtime.NewUnversionedType("CredA")
	typeB := runtime.NewVersionedType("CredB", "v1")

	tests := []struct {
		name    string
		first   []types.Type
		second  []types.Type
	}{
		{
			name:  "two plugins register the same canonical type",
			first: []types.Type{{Type: typeA}},
			second: []types.Type{{Type: typeA}},
		},
		{
			name:  "second plugin's canonical conflicts with first plugin's alias",
			first: []types.Type{{Type: typeA, Aliases: []runtime.Type{aliasA}}},
			second: []types.Type{{Type: aliasA}},
		},
		{
			name:  "second plugin's alias conflicts with first plugin's canonical",
			first: []types.Type{{Type: typeA}},
			second: []types.Type{{Type: typeB, Aliases: []runtime.Type{typeA}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := credentialtype.NewRegistry(t.Context())
			r.NoError(reg.RegisterFromPlugin(tc.first))
			r.Error(reg.RegisterFromPlugin(tc.second))
		})
	}
}

func TestRegisterFromPlugin_CannotOverrideBuiltinTypes(t *testing.T) {
	tests := []struct {
		name   string
		scheme *runtime.Scheme
		plugin types.Type
	}{
		{
			name:   "OCI credentials",
			scheme: ocicredspec.Scheme,
			plugin: types.Type{Type: runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1")},
		},
		{
			name:   "Helm credentials",
			scheme: helmcredspec.Scheme,
			plugin: types.Type{Type: runtime.NewVersionedType(helmcredv1.HelmHTTPCredentialsType, "v1")},
		},
		{
			name:   "RSA credentials",
			scheme: rsacredspec.Scheme,
			plugin: types.Type{Type: rsacredv1.VersionedType},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := credentialtype.NewRegistry(t.Context())
			reg.Register(tc.scheme)
			r.Error(reg.RegisterFromPlugin([]types.Type{tc.plugin}))
		})
	}
}

// TestRegisterFromPlugin_MultipleTypesDoNotConflictWithRaw verifies that registering
// several plugin credential types does not cause them to alias each other through
// *runtime.Raw. Each type must resolve independently: IsRegistered, NewObject, and
// Convert must all behave as if each type were the only one registered.
func TestRegisterFromPlugin_MultipleTypesDoNotConflictWithRaw(t *testing.T) {
	typeA := runtime.NewVersionedType("PluginCredA", "v1")
	typeB := runtime.NewVersionedType("PluginCredB", "v1")
	typeC := runtime.NewVersionedType("PluginCredC", "v2")

	reg := credentialtype.NewRegistry(t.Context())
	require.NoError(t, reg.RegisterFromPlugin([]types.Type{
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

		reg2 := credentialtype.NewRegistry(t.Context())
		r.NoError(reg2.RegisterFromPlugin([]types.Type{
			{Type: aliasedType, Aliases: []runtime.Type{aliasType}},
			{Type: unrelated},
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

func TestBuiltinAndPluginCredentialTypes(t *testing.T) {
	reg := credentialtype.NewRegistry(t.Context())

	reg.Register(ocicredspec.Scheme)
	reg.Register(helmcredspec.Scheme)
	reg.Register(rsacredspec.Scheme)

	pluginTypes := []types.Type{
		{Type: runtime.NewVersionedType("PluginTokenA", "v1")},
		{Type: runtime.NewVersionedType("PluginTokenB", "v1")},
		{Type: runtime.NewVersionedType("PluginTokenC", "v2")},
	}
	require.NoError(t, reg.RegisterFromPlugin(pluginTypes))

	scheme := reg.GetCredentialTypeScheme()

	t.Run("builtin OCI types are registered", func(t *testing.T) {
		r := require.New(t)
		r.True(scheme.IsRegistered(runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1")))
		r.True(scheme.IsRegistered(runtime.NewUnversionedType(ocicredv1.OCICredentialsType)))
		r.True(scheme.IsRegistered(ocicredspec.CredentialRepositoryConfigType))
		r.True(scheme.IsRegistered(runtime.NewUnversionedType("DockerConfig")))
	})

	t.Run("builtin Helm types are registered", func(t *testing.T) {
		r := require.New(t)
		r.True(scheme.IsRegistered(runtime.NewVersionedType(helmcredv1.HelmHTTPCredentialsType, "v1")))
		r.True(scheme.IsRegistered(runtime.NewUnversionedType(helmcredv1.HelmHTTPCredentialsType)))
	})

	t.Run("builtin RSA types are registered", func(t *testing.T) {
		r := require.New(t)
		r.True(scheme.IsRegistered(rsacredv1.VersionedType))
		r.True(scheme.IsRegistered(runtime.NewUnversionedType(rsacredv1.RSACredentialsType)))
	})

	t.Run("plugin types are registered", func(t *testing.T) {
		r := require.New(t)
		for _, pt := range pluginTypes {
			r.True(scheme.IsRegistered(pt.Type), "expected %s to be registered", pt.Type)
		}
	})

	t.Run("NewObject returns concrete type for builtins", func(t *testing.T) {
		r := require.New(t)

		obj, err := scheme.NewObject(runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1"))
		r.NoError(err)
		_, ok := obj.(*ocicredv1.OCICredentials)
		r.True(ok, "expected *ocicredv1.OCICredentials, got %T", obj)

		obj, err = scheme.NewObject(runtime.NewVersionedType(helmcredv1.HelmHTTPCredentialsType, "v1"))
		r.NoError(err)
		_, ok = obj.(*helmcredv1.HelmHTTPCredentials)
		r.True(ok, "expected *helmcredv1.HelmHTTPCredentials, got %T", obj)

		obj, err = scheme.NewObject(rsacredv1.VersionedType)
		r.NoError(err)
		_, ok = obj.(*rsacredv1.RSACredentials)
		r.True(ok, "expected *rsacredv1.RSACredentials, got %T", obj)
	})

	t.Run("NewObject returns *runtime.Raw for plugin types", func(t *testing.T) {
		r := require.New(t)
		for _, pt := range pluginTypes {
			obj, err := scheme.NewObject(pt.Type)
			r.NoError(err, "NewObject failed for %s", pt.Type)
			_, ok := obj.(*runtime.Raw)
			r.True(ok, "expected *runtime.Raw for plugin type %s, got %T", pt.Type, obj)
		}
	})

	t.Run("Convert round-trips builtin OCI credentials", func(t *testing.T) {
		r := require.New(t)

		ociCreds := &ocicredv1.OCICredentials{
			Type:     runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1"),
			Username: "user",
			Password: "pass",
		}
		raw, err := marshalToRaw(ociCreds)
		r.NoError(err)

		into, err := scheme.NewObject(raw.GetType())
		r.NoError(err)
		r.NoError(scheme.Convert(raw, into))

		result, ok := into.(*ocicredv1.OCICredentials)
		r.True(ok)
		r.Equal("user", result.Username)
		r.Equal("pass", result.Password)
	})

	t.Run("Convert round-trips plugin credentials as *runtime.Raw", func(t *testing.T) {
		r := require.New(t)

		pluginType := runtime.NewVersionedType("PluginTokenA", "v1")
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

	t.Run("builtin and plugin types do not interfere", func(t *testing.T) {
		r := require.New(t)

		pluginObj, err := scheme.NewObject(runtime.NewVersionedType("PluginTokenB", "v1"))
		r.NoError(err)
		_, isRaw := pluginObj.(*runtime.Raw)
		r.True(isRaw)

		ociObj, err := scheme.NewObject(runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1"))
		r.NoError(err)
		_, isRaw = ociObj.(*runtime.Raw)
		r.False(isRaw, "OCI builtin type should not resolve to *runtime.Raw")
	})
}

func marshalToRaw(typed runtime.Typed) (*runtime.Raw, error) {
	data, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	raw := &runtime.Raw{}
	raw.SetType(typed.GetType())
	raw.Data = data
	return raw, nil
}
