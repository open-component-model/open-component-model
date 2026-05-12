package credentialplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegisterInternalCredentialPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	reg := NewRegistry(ctx)
	plugin := &mockCredentialPlugin{}
	r.NoError(reg.RegisterInternalCredentialPlugin(plugin))

	tests := []struct {
		name string
		spec runtime.Typed
		err  require.ErrorAssertionFunc
	}{
		{
			name: "prototype",
			spec: &dummyv1.Repository{},
			err:  require.NoError,
		},
		{
			name: "canonical type",
			spec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			spec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			spec: &runtime.Raw{
				Type: runtime.Type{
					Name:    "NonExistingType",
					Version: "v1",
				},
			},
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reg.GetCredentialPlugin(ctx, tc.spec)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(got)
			r.Equal(plugin, got)

			identity, err := got.GetConsumerIdentity(ctx, tc.spec)
			require.NoError(t, err)
			require.Equal(t, runtime.Identity{"type": "stub"}, identity)
			require.True(t, plugin.identityCalled)

			creds, err := got.Resolve(ctx, identity, nil)
			require.NoError(t, err)
			require.Equal(t, map[string]string{"token": "resolved"}, creds)
			require.True(t, plugin.resolveCalled)

			plugin.identityCalled = false
			plugin.resolveCalled = false
		})
	}
}

func TestRegistry_LookupUnregisteredType(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	raw := &runtime.Raw{}
	raw.SetType(runtime.NewVersionedType("Unknown", "v1"))
	_, err := reg.GetCredentialPlugin(t.Context(), raw)
	r.Error(err)
	r.Contains(err.Error(), "no credential plugin registered")
}

func TestRegistry_LookupNilTyped(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	_, err := reg.GetCredentialPlugin(t.Context(), nil)
	r.Error(err)
	r.Contains(err.Error(), "non-nil")
}

func TestRegistry_LookupEmptyType(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	raw := &runtime.Raw{}
	_, err := reg.GetCredentialPlugin(t.Context(), raw)
	r.Error(err)
	r.Contains(err.Error(), "requires a type")
}

func TestRegistry_RegisterNilPlugin(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	err := reg.RegisterInternalCredentialPlugin(nil)
	r.Error(err)
	r.Contains(err.Error(), "nil credential plugin")
}

func TestRegistry_RegisterDuplicatePlugin(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	plugin := &mockCredentialPlugin{}
	r.NoError(reg.RegisterInternalCredentialPlugin(plugin))

	err := reg.RegisterInternalCredentialPlugin(plugin)
	r.Error(err)
	r.Contains(err.Error(), "failed to register provider type")
}

type mockCredentialPlugin struct {
	identityCalled bool
	resolveCalled  bool
}

var _ credentials.CredentialPlugin = (*mockCredentialPlugin)(nil)

func (m *mockCredentialPlugin) GetCredentialPluginScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockCredentialPlugin) GetConsumerIdentity(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	m.identityCalled = true
	return runtime.Identity{"type": "stub"}, nil
}

func (m *mockCredentialPlugin) Resolve(_ context.Context, _ runtime.Identity, _ map[string]string) (map[string]string, error) {
	m.resolveCalled = true
	return map[string]string{"token": "resolved"}, nil
}
