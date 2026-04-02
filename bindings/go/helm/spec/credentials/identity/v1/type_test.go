package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestToIdentity(t *testing.T) {
	id := &HelmChartRepositoryIdentity{
		Hostname: "charts.example.com",
		Scheme:   "https",
		Port:     "443",
		Path:     "/charts",
	}

	identity := id.ToIdentity()

	assert.Equal(t, VersionedType.String(), identity[runtime.IdentityAttributeType])
	assert.Equal(t, "charts.example.com", identity[runtime.IdentityAttributeHostname])
	assert.Equal(t, "https", identity[runtime.IdentityAttributeScheme])
	assert.Equal(t, "443", identity[runtime.IdentityAttributePort])
	assert.Equal(t, "/charts", identity[runtime.IdentityAttributePath])
}

func TestToIdentity_MinimalFields(t *testing.T) {
	id := &HelmChartRepositoryIdentity{
		Hostname: "charts.example.com",
	}

	identity := id.ToIdentity()

	assert.Equal(t, VersionedType.String(), identity[runtime.IdentityAttributeType])
	assert.Equal(t, "charts.example.com", identity[runtime.IdentityAttributeHostname])
	_, hasScheme := identity[runtime.IdentityAttributeScheme]
	assert.False(t, hasScheme, "scheme should not be set when empty")
	_, hasPort := identity[runtime.IdentityAttributePort]
	assert.False(t, hasPort, "port should not be set when empty")
}

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	// Versioned type should be registered
	assert.True(t, scheme.IsRegistered(VersionedType))

	// Unversioned alias should also be registered
	assert.True(t, scheme.IsRegistered(Type))

	// Should create the right object
	obj, err := scheme.NewObject(Type)
	require.NoError(t, err)
	_, ok := obj.(*HelmChartRepositoryIdentity)
	assert.True(t, ok, "expected *HelmChartRepositoryIdentity, got %T", obj)
}

func TestIdentityProvider(t *testing.T) {
	id := &HelmChartRepositoryIdentity{
		Hostname: "charts.example.com",
	}

	// Verify it implements runtime.IdentityProvider
	var _ runtime.IdentityProvider = id

	identity := id.ToIdentity()
	assert.NotNil(t, identity)
}

func TestCredentialAcceptor(t *testing.T) {
	id := &HelmChartRepositoryIdentity{}

	// Verify it implements runtime.CredentialAcceptor
	var _ runtime.CredentialAcceptor = id

	accepted := id.AcceptedCredentialTypes()
	require.Len(t, accepted, 1)
	assert.Equal(t, runtime.NewVersionedType("HelmHTTPCredentials", "v1"), accepted[0])
}
