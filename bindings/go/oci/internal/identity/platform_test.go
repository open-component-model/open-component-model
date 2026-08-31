package identity_test

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPlatformFromIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		expected *ociImageSpecV1.Platform
	}{
		{
			name:     "no platform attributes",
			identity: runtime.Identity{"name": "cli", "version": "1.0.0"},
			expected: nil,
		},
		{
			name:     "nil identity",
			identity: nil,
			expected: nil,
		},
		{
			name:     "architecture and os",
			identity: runtime.Identity{"name": "cli", "architecture": "arm64", "os": "linux"},
			expected: &ociImageSpecV1.Platform{Architecture: "arm64", OS: "linux"},
		},
		{
			name:     "architecture only",
			identity: runtime.Identity{"architecture": "amd64"},
			expected: &ociImageSpecV1.Platform{Architecture: "amd64"},
		},
		{
			name:     "variant",
			identity: runtime.Identity{"architecture": "arm", "os": "linux", "variant": "v7"},
			expected: &ociImageSpecV1.Platform{Architecture: "arm", OS: "linux", Variant: "v7"},
		},
		{
			name:     "os version and features",
			identity: runtime.Identity{"os": "windows", "os.version": "10.0.17763.1", "os.features": "win32k,foo"},
			expected: &ociImageSpecV1.Platform{OS: "windows", OSVersion: "10.0.17763.1", OSFeatures: []string{"win32k", "foo"}},
		},
		{
			name:     "empty values are ignored",
			identity: runtime.Identity{"architecture": "", "os": ""},
			expected: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, identity.PlatformFromIdentity(tc.identity))
		})
	}
}

func TestPlatformFromIdentity_RoundTripsAdopt(t *testing.T) {
	extraIdentity := runtime.Identity{"architecture": "arm64", "os": "linux", "variant": "v8"}

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta:    descriptor.ObjectMeta{Name: "cli", Version: "1.0.0"},
			ExtraIdentity: extraIdentity,
		},
	}

	desc := &ociImageSpecV1.Descriptor{}
	require.NoError(t, identity.Adopt(desc, res))

	assert.Equal(t, desc.Platform, identity.PlatformFromIdentity(res.ToIdentity()))
}
