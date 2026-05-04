package credentialplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type stubPlugin struct {
	scheme *runtime.Scheme
}

func (s *stubPlugin) GetCredentialPluginScheme() *runtime.Scheme {
	return s.scheme
}

func (s *stubPlugin) GetConsumerIdentity(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return runtime.Identity{"type": "stub"}, nil
}

func (s *stubPlugin) Resolve(_ context.Context, _ runtime.Identity, _ map[string]string) (map[string]string, error) {
	return map[string]string{"token": "resolved"}, nil
}

func newStubPlugin(types ...runtime.Type) *stubPlugin {
	scheme := runtime.NewScheme()
	if len(types) > 0 {
		scheme.MustRegisterWithAlias(&runtime.Raw{}, types...)
	}
	return &stubPlugin{scheme: scheme}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := require.New(t)
	reg := NewRegistry(t.Context())

	versionedType := runtime.NewVersionedType("TestPlugin", "v1")
	unversionedType := runtime.NewUnversionedType("TestPlugin")
	plugin := newStubPlugin(versionedType, unversionedType)

	r.NoError(reg.RegisterInternalCredentialPlugin(plugin))

	raw := &runtime.Raw{}
	raw.SetType(versionedType)
	got, err := reg.GetCredentialPlugin(t.Context(), raw)
	r.NoError(err)
	r.Equal(plugin, got)

	raw.SetType(unversionedType)
	got, err = reg.GetCredentialPlugin(t.Context(), raw)
	r.NoError(err)
	r.Equal(plugin, got)
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
