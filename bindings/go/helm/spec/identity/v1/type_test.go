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

	assert.True(t, scheme.IsRegistered(VersionedType))
	assert.True(t, scheme.IsRegistered(Type))

	obj, err := scheme.NewObject(Type)
	require.NoError(t, err)
	_, ok := obj.(*HelmChartRepositoryIdentity)
	assert.True(t, ok, "expected *HelmChartRepositoryIdentity, got %T", obj)
}

func TestHelmChartRepositoryIdentity_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterIdentityType(scheme)

	original := &HelmChartRepositoryIdentity{
		Type:     VersionedType,
		Hostname: "charts.example.com",
		Scheme:   "https",
		Port:     "443",
		Path:     "/stable",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &HelmChartRepositoryIdentity{}
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
		input *HelmChartRepositoryIdentity
		want  runtime.Identity
	}{
		{
			name: "full identity",
			input: &HelmChartRepositoryIdentity{
				Type:     VersionedType,
				Hostname: "charts.example.com",
				Scheme:   "https",
				Port:     "443",
				Path:     "stable",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "charts.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "stable",
			},
		},
		{
			name: "only hostname",
			input: &HelmChartRepositoryIdentity{
				Type:     VersionedType,
				Hostname: "charts.example.com",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "charts.example.com",
			},
		},
		{
			name:  "empty identity uses defaulted type",
			input: &HelmChartRepositoryIdentity{},
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
		want  *HelmChartRepositoryIdentity
	}{
		{
			name: "full identity",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "charts.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "stable",
			},
			want: &HelmChartRepositoryIdentity{
				Type:     VersionedType,
				Hostname: "charts.example.com",
				Scheme:   "https",
				Port:     "443",
				Path:     "stable",
			},
		},
		{
			name: "only hostname",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "charts.example.com",
			},
			want: &HelmChartRepositoryIdentity{
				Type:     VersionedType,
				Hostname: "charts.example.com",
			},
		},
		{
			name: "unknown attributes are ignored",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "charts.example.com",
				"unrelated":                       "value",
			},
			want: &HelmChartRepositoryIdentity{
				Type:     VersionedType,
				Hostname: "charts.example.com",
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
		original := &HelmChartRepositoryIdentity{
			Type:     VersionedType,
			Hostname: "charts.example.com",
			Scheme:   "https",
			Port:     "443",
			Path:     "stable",
		}
		assert.Equal(t, original, FromIdentity(ToIdentity(original)))
	})

	t.Run("identity -> struct -> identity", func(t *testing.T) {
		original := runtime.Identity{
			runtime.IdentityAttributeType:     VersionedType.String(),
			runtime.IdentityAttributeHostname: "charts.example.com",
			runtime.IdentityAttributeScheme:   "https",
			runtime.IdentityAttributePort:     "443",
			runtime.IdentityAttributePath:     "stable",
		}
		assert.Equal(t, original, ToIdentity(FromIdentity(original)))
	})
}
