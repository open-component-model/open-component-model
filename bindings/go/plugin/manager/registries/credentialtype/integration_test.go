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

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestBuiltinAndPluginCredentialTypes is a full integration test that registers
// built-in credential schemes (OCI, Helm, RSA) alongside plugin-declared types,
// then exercises IsRegistered, NewObject, and Convert for all of them.
func TestBuiltinAndPluginCredentialTypes(t *testing.T) {
	reg := credentialtype.NewRegistry(t.Context())

	// --- register built-in schemes ---
	reg.Register(ocicredspec.Scheme)
	reg.Register(helmcredspec.Scheme)
	reg.Register(rsacredspec.Scheme)

	// --- register plugin-declared types ---
	pluginTypes := []types.Type{
		{Type: runtime.NewVersionedType("PluginTokenA", "v1")},
		{Type: runtime.NewVersionedType("PluginTokenB", "v1")},
		{Type: runtime.NewVersionedType("PluginTokenC", "v2")},
	}
	reg.RegisterFromPlugin(pluginTypes)

	scheme := reg.GetCredentialTypeScheme()

	t.Run("builtin OCI types are registered", func(t *testing.T) {
		r := require.New(t)
		r.True(scheme.IsRegistered(runtime.NewVersionedType(ocicredv1.OCICredentialsType, "v1")))
		r.True(scheme.IsRegistered(runtime.NewUnversionedType(ocicredv1.OCICredentialsType)))
		r.True(scheme.IsRegistered(ocicredspec.CredentialRepositoryConfigType)) // DockerConfig/v1
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

		// A plugin type must not resolve to a builtin Go type
		pluginObj, err := scheme.NewObject(runtime.NewVersionedType("PluginTokenB", "v1"))
		r.NoError(err)
		_, isRaw := pluginObj.(*runtime.Raw)
		r.True(isRaw)

		// A builtin type must not resolve to *runtime.Raw
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
