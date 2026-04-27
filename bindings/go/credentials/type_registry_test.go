package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIdentityTypeRegistry_Register(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	typ := runtime.NewVersionedType("OCIRegistry", "v1")

	err := registry.Register(&runtime.Raw{}, typ)
	require.NoError(t, err)
	assert.True(t, registry.Scheme().IsRegistered(typ))
}

func TestIdentityTypeRegistry_RegisterWithAcceptedCredentials(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	identityType := runtime.NewVersionedType("HelmChartRepository", "v1")
	identityAlias := runtime.NewUnversionedType("HelmChartRepository")
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")

	err := registry.RegisterWithAcceptedCredentials(
		&runtime.Raw{},
		[]runtime.Type{identityType, identityAlias},
		[]runtime.Type{credType},
	)
	require.NoError(t, err)

	// Both default and alias are registered in the scheme.
	assert.True(t, registry.Scheme().IsRegistered(identityType))
	assert.True(t, registry.Scheme().IsRegistered(identityAlias))

	// Accepted credential types are queryable by default type.
	accepted, ok := registry.AcceptedCredentialTypes(identityType)
	require.True(t, ok)
	assert.Equal(t, []runtime.Type{credType}, accepted)
}

func TestIdentityTypeRegistry_AcceptedCredentialTypes_AliasResolution(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	identityType := runtime.NewVersionedType("HelmChartRepository", "v1")
	identityAlias := runtime.NewUnversionedType("HelmChartRepository")
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")

	err := registry.RegisterWithAcceptedCredentials(
		&runtime.Raw{},
		[]runtime.Type{identityType, identityAlias},
		[]runtime.Type{credType},
	)
	require.NoError(t, err)

	// Querying by alias should resolve to the same accepted types.
	accepted, ok := registry.AcceptedCredentialTypes(identityAlias)
	require.True(t, ok)
	assert.Equal(t, []runtime.Type{credType}, accepted)
}

func TestIdentityTypeRegistry_AcceptedCredentialTypes_NotRegistered(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	unknown := runtime.NewVersionedType("Unknown", "v1")

	accepted, ok := registry.AcceptedCredentialTypes(unknown)
	assert.False(t, ok)
	assert.Nil(t, accepted)
}

func TestIdentityTypeRegistry_RegisterWithoutAcceptedCredentials(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	typ := runtime.NewVersionedType("OCIRegistry", "v1")

	err := registry.RegisterWithAcceptedCredentials(
		&runtime.Raw{},
		[]runtime.Type{typ},
		nil, // no accepted credential types
	)
	require.NoError(t, err)

	// Type is registered in scheme.
	assert.True(t, registry.Scheme().IsRegistered(typ))

	// No accepted credential types stored.
	_, ok := registry.AcceptedCredentialTypes(typ)
	assert.False(t, ok)
}

func TestIdentityTypeRegistry_Scheme_SatisfiesTypeSchemeProvider(t *testing.T) {
	registry := NewIdentityTypeRegistry()

	// IdentityTypeRegistry implements TypeSchemeProvider.
	var provider TypeSchemeProvider = registry
	assert.NotNil(t, provider.Scheme())
}

func TestIdentityTypeRegistry_DuplicateRegistration(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	typ := runtime.NewVersionedType("OCIRegistry", "v1")

	require.NoError(t, registry.Register(&runtime.Raw{}, typ))
	err := registry.Register(&runtime.Raw{}, typ)
	require.Error(t, err)
	assert.True(t, runtime.IsTypeAlreadyRegisteredError(err))
}
