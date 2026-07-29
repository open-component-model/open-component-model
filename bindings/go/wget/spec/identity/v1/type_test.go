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
	_, ok := obj.(*WgetIdentity)
	assert.True(t, ok, "expected *WgetIdentity, got %T", obj)
}

func TestWgetIdentity_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterIdentityType(scheme)

	original := &WgetIdentity{
		Type:     VersionedType,
		Hostname: "downloads.example.com",
		Scheme:   "https",
		Port:     "443",
		Path:     "myapp/1.0.0/myapp.tar.gz",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &WgetIdentity{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Hostname, restored.Hostname)
	assert.Equal(t, original.Scheme, restored.Scheme)
	assert.Equal(t, original.Port, restored.Port)
	assert.Equal(t, original.Path, restored.Path)
}

// TestIdentityFromURL pins the consumer identity that the access type and the
// input method both resolve for a resource URL. The type is the unversioned
// form, consistent with the OCIRegistry and HelmChartRepository identities.
func TestIdentityFromURL(t *testing.T) {
	got, err := IdentityFromURL("https://downloads.example.com/myapp/1.0.0/myapp.tar.gz")
	require.NoError(t, err)

	assert.Equal(t, runtime.Identity{
		runtime.IdentityAttributeType:     Type.String(),
		runtime.IdentityAttributeHostname: "downloads.example.com",
		runtime.IdentityAttributeScheme:   "https",
		runtime.IdentityAttributePath:     "myapp/1.0.0/myapp.tar.gz",
	}, got)
	assert.Equal(t, "Wget", got[runtime.IdentityAttributeType])
}

// TestIdentityFromURL_MatchesOnlyUnversionedConsumer pins the exact-match
// semantics: consumer identity types are compared by exact string, so only the
// unversioned spelling resolves. This mirrors OCIRegistry and
// HelmChartRepository, where the versioned spelling does not match either.
func TestIdentityFromURL_MatchesOnlyUnversionedConsumer(t *testing.T) {
	lookup, err := IdentityFromURL("https://downloads.example.com/myapp/1.0.0/myapp.tar.gz")
	require.NoError(t, err)

	for _, tt := range []struct {
		consumerType string
		want         bool
	}{
		{"Wget", true},
		{"Wget/v1", false},
		{"wget", false},
	} {
		t.Run(tt.consumerType, func(t *testing.T) {
			consumer := runtime.Identity{
				runtime.IdentityAttributeType:     tt.consumerType,
				runtime.IdentityAttributeHostname: "downloads.example.com",
			}
			assert.Equal(t, tt.want, lookup.Clone().Match(consumer.Clone()))
		})
	}
}
