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
