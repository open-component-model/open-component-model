package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	props := map[string]string{
		"username": "myuser",
		"password": "mypass",
		"certFile": "/path/to/cert.pem",
		"keyFile":  "/path/to/key.pem",
		"keyring":  "/path/to/keyring",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "myuser", creds.Username)
	assert.Equal(t, "mypass", creds.Password)
	assert.Equal(t, "/path/to/cert.pem", creds.CertFile)
	assert.Equal(t, "/path/to/key.pem", creds.KeyFile)
	assert.Equal(t, "/path/to/keyring", creds.Keyring)
	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
}

func TestFromDirectCredentials_PartialFields(t *testing.T) {
	props := map[string]string{
		"username": "user",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "user", creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

func TestFromDirectCredentials_EmptyMap(t *testing.T) {
	creds := FromDirectCredentials(map[string]string{})

	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
	assert.Empty(t, creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

func TestFromDirectCredentials_NilMap(t *testing.T) {
	creds := FromDirectCredentials(nil)

	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
	assert.Empty(t, creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(HelmHTTPCredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &HelmHTTPCredentials{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(HelmHTTPCredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &HelmHTTPCredentials{}, obj)
}

func TestHelmHTTPCredentials_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &HelmHTTPCredentials{
		Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
		Username: "testuser",
		Password: "testpass",
		CertFile: "/path/cert.pem",
		KeyFile:  "/path/key.pem",
		Keyring:  "/path/keyring",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &HelmHTTPCredentials{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Username, restored.Username)
	assert.Equal(t, original.Password, restored.Password)
	assert.Equal(t, original.CertFile, restored.CertFile)
	assert.Equal(t, original.KeyFile, restored.KeyFile)
	assert.Equal(t, original.Keyring, restored.Keyring)
}
