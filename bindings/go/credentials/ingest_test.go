package credentials

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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

func Test_extractResolvable_DirectCredentials_NilProperties_NoPanic(t *testing.T) {
	g := &Graph{}

	// First DirectCredentials entry has no properties (nil map).
	dc1 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1"}`),
	}
	// Second DirectCredentials entry has properties — would panic on maps.Copy
	// if the accumulator's Properties map is nil.
	dc2 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"username":"admin"}}`),
	}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{dc1, dc2})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	require.NotNil(t, resolved)
	dc, ok := resolved.(*v1.DirectCredentials)
	require.True(t, ok)
	assert.Equal(t, "admin", dc.Properties["username"])
}

// staticSchemeProvider is a test helper implementing CredentialTypeSchemeProvider.
type staticSchemeProvider struct {
	scheme *runtime.Scheme
}

func (s *staticSchemeProvider) GetCredentialTypeScheme() *runtime.Scheme {
	return s.scheme
}
