package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// helmHTTPCredentials is a stand-in typed credential for tests covering the
// merge path. It mirrors the shape a binding-defined credential would take.
type helmHTTPCredentials struct {
	Type     runtime.Type `json:"type"`
	CertFile string       `json:"certFile,omitempty"`
	KeyFile  string       `json:"keyFile,omitempty"`
}

func (h *helmHTTPCredentials) GetType() runtime.Type    { return h.Type }
func (h *helmHTTPCredentials) SetType(typ runtime.Type) { h.Type = typ }
func (h *helmHTTPCredentials) DeepCopyTyped() runtime.Typed {
	cp := *h
	return &cp
}

var helmHTTPCredentialsType = runtime.NewVersionedType("HelmHTTPCredentials", "v1")

func newSchemeWith(t *testing.T, prototypes ...runtime.Typed) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	v1.MustRegister(s)
	for _, p := range prototypes {
		require.NoError(t, s.RegisterWithAlias(p, p.GetType()))
	}
	return s
}

func TestMergeTyped_Empty(t *testing.T) {
	got, err := mergeTyped(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestMergeTyped_SinglePassesThroughUntouched(t *testing.T) {
	cred := &helmHTTPCredentials{
		Type:     helmHTTPCredentialsType,
		CertFile: "/etc/cert",
	}
	// No scheme: single-result path must not require one.
	got, err := mergeTyped([]runtime.Typed{cred}, nil)
	require.NoError(t, err)
	assert.Same(t, cred, got, "single result must be returned as-is to preserve concrete type")
}

func TestMergeTyped_MultipleMergesIntoDirectCredentials(t *testing.T) {
	scheme := newSchemeWith(t, &helmHTTPCredentials{Type: helmHTTPCredentialsType})

	first := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"username": "abc"},
	}
	second := &helmHTTPCredentials{
		Type:     helmHTTPCredentialsType,
		CertFile: "/etc/cert",
		KeyFile:  "/etc/key",
	}

	got, err := mergeTyped([]runtime.Typed{first, second}, scheme)
	require.NoError(t, err)

	dc, ok := got.(*v1.DirectCredentials)
	require.True(t, ok, "expected *v1.DirectCredentials, got %T", got)
	assert.Equal(t, "abc", dc.Properties["username"])
	assert.Equal(t, "/etc/cert", dc.Properties["certFile"])
	assert.Equal(t, "/etc/key", dc.Properties["keyFile"])
	assert.NotContains(t, dc.Properties, "type", "type field must not leak into properties")
}

func TestMergeTyped_LaterOverridesEarlierOnSameKey(t *testing.T) {
	scheme := newSchemeWith(t)

	first := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"username": "first", "shared": "early"},
	}
	second := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"shared": "late", "extra": "yes"},
	}

	got, err := mergeTyped([]runtime.Typed{first, second}, scheme)
	require.NoError(t, err)

	dc := got.(*v1.DirectCredentials)
	assert.Equal(t, "first", dc.Properties["username"])
	assert.Equal(t, "late", dc.Properties["shared"], "later entries must override earlier ones")
	assert.Equal(t, "yes", dc.Properties["extra"])
}

func TestMergeTyped_MultipleWithoutSchemeFails(t *testing.T) {
	first := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"username": "abc"},
	}
	second := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"password": "def"},
	}

	got, err := mergeTyped([]runtime.Typed{first, second}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "scheme is nil", "error should indicate that scheme is required when merging multiple credentials")
	assert.Nil(t, got)
}

func TestMergeTyped_UnregisteredTypeFails(t *testing.T) {
	// scheme knows DirectCredentials but not helmHTTPCredentials.
	scheme := newSchemeWith(t)

	known := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: map[string]string{"username": "abc"},
	}
	unknown := &helmHTTPCredentials{
		Type:     helmHTTPCredentialsType,
		CertFile: "/etc/cert",
	}

	got, err := mergeTyped([]runtime.Typed{known, unknown}, scheme)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), helmHTTPCredentialsType.String(), "error should name the unregistered type")
}
