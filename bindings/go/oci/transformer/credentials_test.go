package transformer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type stubResolver struct {
	typed runtime.Typed
	err   error
}

func (s *stubResolver) Resolve(_ context.Context, _ runtime.Identity) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubResolver) ResolveTyped(_ context.Context, _ runtime.Typed) (runtime.Typed, error) {
	return s.typed, s.err
}

func TestResolveCredentialsMap_OCICredentials(t *testing.T) {
	resolver := &stubResolver{
		typed: &ocicredsv1.OCICredentials{
			Username:     "user",
			Password:     "pass",
			AccessToken:  "tok",
			RefreshToken: "ref",
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.NoError(t, err)
	assert.Equal(t, "user", creds[ocicredsv1.CredentialKeyUsername])
	assert.Equal(t, "pass", creds[ocicredsv1.CredentialKeyPassword])
	assert.Equal(t, "tok", creds[ocicredsv1.CredentialKeyAccessToken])
	assert.Equal(t, "ref", creds[ocicredsv1.CredentialKeyRefreshToken])
}

func TestResolveCredentialsMap_OCICredentials_SkipsEmptyFields(t *testing.T) {
	resolver := &stubResolver{
		typed: &ocicredsv1.OCICredentials{
			Username: "user",
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.NoError(t, err)
	assert.Equal(t, "user", creds[ocicredsv1.CredentialKeyUsername])
	_, hasPassword := creds[ocicredsv1.CredentialKeyPassword]
	assert.False(t, hasPassword, "empty password should not be in map")
	_, hasAccessToken := creds[ocicredsv1.CredentialKeyAccessToken]
	assert.False(t, hasAccessToken, "empty accessToken should not be in map")
	_, hasRefreshToken := creds[ocicredsv1.CredentialKeyRefreshToken]
	assert.False(t, hasRefreshToken, "empty refreshToken should not be in map")
}

func TestResolveCredentialsMap_DirectCredentials(t *testing.T) {
	resolver := &stubResolver{
		typed: &credconfigv1.DirectCredentials{
			Properties: map[string]string{"username": "direct-user", "password": "direct-pass"},
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.NoError(t, err)
	assert.Equal(t, "direct-user", creds["username"])
	assert.Equal(t, "direct-pass", creds["password"])
}

func TestResolveCredentialsMap_NotFound(t *testing.T) {
	resolver := &stubResolver{err: credentials.ErrNotFound}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestResolveCredentialsMap_Error(t *testing.T) {
	resolver := &stubResolver{err: fmt.Errorf("vault unavailable")}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault unavailable")
	assert.Nil(t, creds)
}

func TestResolveCredentialsMap_UnknownType(t *testing.T) {
	resolver := &stubResolver{typed: &runtime.Raw{}}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "OCIRegistry"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential type")
	assert.Nil(t, creds)
}
