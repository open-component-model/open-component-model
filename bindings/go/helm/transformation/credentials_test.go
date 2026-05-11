package transformation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type stubResolver struct {
	typed runtime.Typed
	err   error
}

func (s *stubResolver) Resolve(_ context.Context, _ runtime.Identity) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubResolver) ResolveTyped(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	return s.typed, s.err
}

func TestResolveCredentialsMap_HelmHTTPCredentials(t *testing.T) {
	resolver := &stubResolver{
		typed: &helmcredsv1.HelmHTTPCredentials{
			Username: "user",
			Password: "pass",
			CertFile: "/path/cert.pem",
			KeyFile:  "/path/key.pem",
			Keyring:  "/path/keyring",
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Equal(t, "user", creds[helmcredsv1.CredentialKeyUsername])
	assert.Equal(t, "pass", creds[helmcredsv1.CredentialKeyPassword])
	assert.Equal(t, "/path/cert.pem", creds[helmcredsv1.CredentialKeyCertFile])
	assert.Equal(t, "/path/key.pem", creds[helmcredsv1.CredentialKeyKeyFile])
	assert.Equal(t, "/path/keyring", creds[helmcredsv1.CredentialKeyKeyring])
}

func TestResolveCredentialsMap_HelmHTTPCredentials_SkipsEmptyFields(t *testing.T) {
	resolver := &stubResolver{
		typed: &helmcredsv1.HelmHTTPCredentials{
			Username: "user",
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Equal(t, "user", creds[helmcredsv1.CredentialKeyUsername])
	_, hasPassword := creds[helmcredsv1.CredentialKeyPassword]
	assert.False(t, hasPassword, "empty password should not be in map")
	_, hasCertFile := creds[helmcredsv1.CredentialKeyCertFile]
	assert.False(t, hasCertFile, "empty certFile should not be in map")
	_, hasKeyFile := creds[helmcredsv1.CredentialKeyKeyFile]
	assert.False(t, hasKeyFile, "empty keyFile should not be in map")
	_, hasKeyring := creds[helmcredsv1.CredentialKeyKeyring]
	assert.False(t, hasKeyring, "empty keyring should not be in map")
}

func TestResolveCredentialsMap_HelmHTTPCredentials_AllEmpty(t *testing.T) {
	resolver := &stubResolver{
		typed: &helmcredsv1.HelmHTTPCredentials{},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Nil(t, creds, "all-empty credentials should return nil")
}

func TestResolveCredentialsMap_DirectCredentials(t *testing.T) {
	resolver := &stubResolver{
		typed: &credconfigv1.DirectCredentials{
			Properties: map[string]string{"username": "direct-user", "password": "direct-pass"},
		},
	}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Equal(t, "direct-user", creds["username"])
	assert.Equal(t, "direct-pass", creds["password"])
}

func TestResolveCredentialsMap_NotFound(t *testing.T) {
	resolver := &stubResolver{err: credentials.ErrNotFound}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestResolveCredentialsMap_Error(t *testing.T) {
	resolver := &stubResolver{err: fmt.Errorf("vault unavailable")}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault unavailable")
	assert.Nil(t, creds)
}

func TestResolveCredentialsMap_NilTyped(t *testing.T) {
	resolver := &stubResolver{typed: nil, err: nil}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.NoError(t, err)
	assert.Nil(t, creds, "nil typed should return nil, nil")
}

func TestResolveCredentialsMap_UnknownType(t *testing.T) {
	resolver := &stubResolver{typed: &runtime.Raw{}}

	creds, err := resolveCredentialsMap(t.Context(), resolver, runtime.Identity{"type": "HelmChartRepository"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential type")
	assert.Nil(t, creds)
}
