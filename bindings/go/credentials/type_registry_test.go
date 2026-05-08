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

	require.NoError(t, registry.Register(typ))
	assert.True(t, registry.IsRegistered(typ))
}

func TestIdentityTypeRegistry_RegisterWithAcceptedCredentials(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	identityType := runtime.NewVersionedType("HelmChartRepository", "v1")
	identityAlias := runtime.NewUnversionedType("HelmChartRepository")
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")

	err := registry.RegisterWithAcceptedCredentials(
		[]runtime.Type{identityType, identityAlias},
		[]runtime.Type{credType},
	)
	require.NoError(t, err)

	// Both default and alias are registered.
	assert.True(t, registry.IsRegistered(identityType))
	assert.True(t, registry.IsRegistered(identityAlias))

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
		[]runtime.Type{typ},
		nil, // no accepted credential types
	)
	require.NoError(t, err)

	assert.True(t, registry.IsRegistered(typ))

	// No accepted credential types stored.
	_, ok := registry.AcceptedCredentialTypes(typ)
	assert.False(t, ok)
}

func TestIdentityTypeRegistry_DuplicateAliasError(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	canonicalA := runtime.NewVersionedType("OCIRegistry", "v1")
	canonicalB := runtime.NewVersionedType("Other", "v1")
	alias := runtime.NewUnversionedType("OCIRegistry")

	require.NoError(t, registry.Register(canonicalA, alias))

	// Reusing the same alias for a different canonical must fail.
	err := registry.Register(canonicalB, alias)
	require.Error(t, err)
	assert.True(t, runtime.IsTypeAlreadyRegisteredError(err))
}

func TestIdentityTypeRegistry_IdempotentRegistration(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	typ := runtime.NewVersionedType("OCIRegistry", "v1")
	alias := runtime.NewUnversionedType("OCIRegistry")

	require.NoError(t, registry.Register(typ, alias))
	// Registering the same canonical/alias pair again is a no-op.
	require.NoError(t, registry.Register(typ, alias))
	assert.True(t, registry.IsRegistered(typ))
	assert.True(t, registry.IsRegistered(alias))
}
