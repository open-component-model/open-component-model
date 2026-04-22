package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestOCIRegistryIdentity_ToIdentity(t *testing.T) {
	tests := []struct {
		name     string
		identity OCIRegistryIdentity
		expected runtime.Identity
	}{
		{
			name: "all fields populated",
			identity: OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
				Scheme:   "https",
				Port:     "443",
				Path:     "v2",
			},
			expected: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "v2",
			},
		},
		{
			name: "hostname only",
			identity: OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "ghcr.io",
			},
			expected: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "ghcr.io",
			},
		},
		{
			name:     "empty identity",
			identity: OCIRegistryIdentity{Type: VersionedType},
			expected: runtime.Identity{
				runtime.IdentityAttributeType: VersionedType.String(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.identity.ToIdentity()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOCIRegistryIdentity_AcceptedCredentialTypes(t *testing.T) {
	identity := &OCIRegistryIdentity{}
	accepted := identity.AcceptedCredentialTypes()

	require.Len(t, accepted, 1)
	assert.Equal(t, runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version), accepted[0])
}

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	// Should resolve versioned type
	obj, err := scheme.NewObject(VersionedType)
	require.NoError(t, err)
	assert.IsType(t, &OCIRegistryIdentity{}, obj)

	// Should resolve unversioned alias
	obj, err = scheme.NewObject(Type)
	require.NoError(t, err)
	assert.IsType(t, &OCIRegistryIdentity{}, obj)
}

func TestOCIRegistryIdentity_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterIdentityType(scheme)

	original := &OCIRegistryIdentity{
		Type:     VersionedType,
		Hostname: "registry.example.com",
		Scheme:   "https",
		Port:     "5000",
		Path:     "my/repo",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &OCIRegistryIdentity{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Hostname, restored.Hostname)
	assert.Equal(t, original.Scheme, restored.Scheme)
	assert.Equal(t, original.Port, restored.Port)
	assert.Equal(t, original.Path, restored.Path)
}
