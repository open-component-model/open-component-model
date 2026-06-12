package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

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
