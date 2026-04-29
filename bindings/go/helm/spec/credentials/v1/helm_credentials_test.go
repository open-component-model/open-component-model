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

	assert.Empty(t, creds.Username)
	assert.Empty(t, creds.Password)
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	// Should be able to create an object from the versioned type
	obj, err := scheme.NewObject(runtime.NewVersionedType(HelmHTTPCredentialsType, Version))
	require.NoError(t, err)
	require.NotNil(t, obj)

	_, ok := obj.(*HelmHTTPCredentials)
	assert.True(t, ok, "expected *HelmHTTPCredentials, got %T", obj)
}
