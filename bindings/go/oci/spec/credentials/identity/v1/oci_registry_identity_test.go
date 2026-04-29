package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

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

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Hostname, restored.Hostname)
	assert.Equal(t, original.Scheme, restored.Scheme)
	assert.Equal(t, original.Port, restored.Port)
	assert.Equal(t, original.Path, restored.Path)
}
