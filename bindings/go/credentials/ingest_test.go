package credentials

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_isAccepted_ExactMatch(t *testing.T) {
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	accepted := []runtime.Type{credType}
	assert.True(t, isAccepted(nil, credType, accepted))
}

func Test_isAccepted_NoMatch(t *testing.T) {
	credType := runtime.NewVersionedType("WrongType", "v1")
	accepted := []runtime.Type{runtime.NewVersionedType("HelmHTTPCredentials", "v1")}
	assert.False(t, isAccepted(nil, credType, accepted))
}

func Test_isAccepted_AliasMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	defaultType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	alias := runtime.NewUnversionedType("HelmHTTPCredentials")
	scheme.MustRegisterWithAlias(&runtime.Raw{}, defaultType, alias)

	// Accepted list declares the versioned type, but the user configured the unversioned alias.
	accepted := []runtime.Type{defaultType}
	assert.True(t, isAccepted(scheme, alias, accepted),
		"unversioned alias should match versioned default via scheme resolution")

	// And vice versa: accepted declares alias, user configured default.
	accepted2 := []runtime.Type{alias}
	assert.True(t, isAccepted(scheme, defaultType, accepted2),
		"versioned default should match unversioned alias via scheme resolution")
}

func Test_isAccepted_NilScheme_FallsBackToExact(t *testing.T) {
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	alias := runtime.NewUnversionedType("HelmHTTPCredentials")
	accepted := []runtime.Type{credType}
	// Without a scheme, alias should NOT match
	assert.False(t, isAccepted(nil, alias, accepted))
}

func Test_isAccepted_WithAllowUnknown_NoFalsePositive(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	accepted := []runtime.Type{runtime.NewVersionedType("Foo", "v1")}
	credType := runtime.NewVersionedType("Bar", "v1")
	// Both types are unknown to the scheme. WithAllowUnknown causes NewObject
	// to return *Raw for any type — isAccepted must not treat them as aliases.
	assert.False(t, isAccepted(scheme, credType, accepted),
		"unrelated unknown types must not match even with WithAllowUnknown")
}

func Test_extractResolvable_MultipleTypedCredentials_FirstWins(t *testing.T) {
	credScheme := runtime.NewScheme()
	type1 := runtime.NewVersionedType("TypeA", "v1")
	type2 := runtime.NewVersionedType("TypeB", "v1")
	credScheme.MustRegisterWithAlias(&runtime.Raw{}, type1)
	credScheme.MustRegisterWithAlias(&runtime.Raw{}, type2)

	g := &Graph{
		credentialTypeSchemeProvider: &staticSchemeProvider{scheme: credScheme},
	}

	cred1 := &runtime.Raw{Type: type1, Data: []byte(`{"type":"TypeA/v1"}`)}
	cred2 := &runtime.Raw{Type: type2, Data: []byte(`{"type":"TypeB/v1"}`)}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{cred1, cred2})
	require.NoError(t, err)

	// First typed credential wins
	require.NotNil(t, resolved)
	assert.Equal(t, type1, resolved.GetType())

	// Second typed credential goes to remaining (not silently dropped)
	assert.Len(t, remaining, 1)
	assert.Equal(t, type2, remaining[0].GetType())
}

func Test_extractResolvable_DirectCredentialsMerge(t *testing.T) {
	g := &Graph{}

	dc1 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"username":"admin"}}`),
	}
	dc2 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"password":"secret"}}`),
	}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{dc1, dc2})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	require.NotNil(t, resolved)
	dc, ok := resolved.(*v1.DirectCredentials)
	require.True(t, ok, "resolved should be DirectCredentials")
	assert.Equal(t, "admin", dc.Properties["username"])
	assert.Equal(t, "secret", dc.Properties["password"])
}

// staticSchemeProvider is a test helper implementing TypeSchemeProvider.
type staticSchemeProvider struct {
	scheme *runtime.Scheme
}

func (s *staticSchemeProvider) Scheme() *runtime.Scheme {
	return s.scheme
}
