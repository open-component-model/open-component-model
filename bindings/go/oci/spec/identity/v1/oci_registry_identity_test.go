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

func TestToIdentity_NilInput(t *testing.T) {
	assert.Nil(t, ToIdentity(nil))
}

func TestFromIdentity_NilInput(t *testing.T) {
	assert.Nil(t, FromIdentity(nil))
}

func TestToIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input *OCIRegistryIdentity
		want  runtime.Identity
	}{
		{
			name: "full identity",
			input: &OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
				Scheme:   "https",
				Port:     "5000",
				Path:     "my/repo",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "5000",
				runtime.IdentityAttributePath:     "my/repo",
			},
		},
		{
			name: "only hostname",
			input: &OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
			},
		},
		{
			name:  "empty identity uses defaulted type",
			input: &OCIRegistryIdentity{},
			want: runtime.Identity{
				runtime.IdentityAttributeType: VersionedType.String(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToIdentity(tt.input))
		})
	}
}

func TestFromIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input runtime.Identity
		want  *OCIRegistryIdentity
	}{
		{
			name: "full identity",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "5000",
				runtime.IdentityAttributePath:     "my/repo",
			},
			want: &OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
				Scheme:   "https",
				Port:     "5000",
				Path:     "my/repo",
			},
		},
		{
			name: "only hostname",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
			},
			want: &OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
			},
		},
		{
			name: "unknown attributes are ignored",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "registry.example.com",
				"unrelated":                       "value",
			},
			want: &OCIRegistryIdentity{
				Type:     VersionedType,
				Hostname: "registry.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FromIdentity(tt.input))
		})
	}
}

// TestIdentity_RoundTrip verifies that ToIdentity followed by FromIdentity
// returns an equivalent struct (and vice versa).
func TestIdentity_RoundTrip(t *testing.T) {
	t.Run("struct -> identity -> struct", func(t *testing.T) {
		original := &OCIRegistryIdentity{
			Type:     VersionedType,
			Hostname: "registry.example.com",
			Scheme:   "https",
			Port:     "5000",
			Path:     "my/repo",
		}
		assert.Equal(t, original, FromIdentity(ToIdentity(original)))
	})

	t.Run("identity -> struct -> identity", func(t *testing.T) {
		original := runtime.Identity{
			runtime.IdentityAttributeType:     VersionedType.String(),
			runtime.IdentityAttributeHostname: "registry.example.com",
			runtime.IdentityAttributeScheme:   "https",
			runtime.IdentityAttributePort:     "5000",
			runtime.IdentityAttributePath:     "my/repo",
		}
		assert.Equal(t, original, ToIdentity(FromIdentity(original)))
	})
}
