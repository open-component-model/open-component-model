package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	props := map[string]string{
		"public_key_pem":       "-----BEGIN PUBLIC KEY-----\nMIIB...",
		"public_key_pem_file":  "/path/to/public.pem",
		"private_key_pem":      "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
		"private_key_pem_file": "/path/to/private.pem",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "-----BEGIN PUBLIC KEY-----\nMIIB...", creds.PublicKeyPEM)
	assert.Equal(t, "/path/to/public.pem", creds.PublicKeyPEMFile)
	assert.Equal(t, "-----BEGIN RSA PRIVATE KEY-----\nMIIE...", creds.PrivateKeyPEM)
	assert.Equal(t, "/path/to/private.pem", creds.PrivateKeyPEMFile)
	assert.Equal(t, runtime.NewVersionedType(RSACredentialsType, Version), creds.Type)
}

func TestFromDirectCredentials_PartialFields(t *testing.T) {
	props := map[string]string{
		"private_key_pem": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "-----BEGIN RSA PRIVATE KEY-----\nMIIE...", creds.PrivateKeyPEM)
	assert.Empty(t, creds.PublicKeyPEM)
	assert.Empty(t, creds.PublicKeyPEMFile)
	assert.Empty(t, creds.PrivateKeyPEMFile)
}

func TestFromDirectCredentials_EmptyMap(t *testing.T) {
	creds := FromDirectCredentials(map[string]string{})

	assert.Empty(t, creds.PublicKeyPEM)
	assert.Empty(t, creds.PrivateKeyPEM)
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(RSACredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(RSACredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)
}

func TestRSACredentials_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &RSACredentials{
		Type:              runtime.NewVersionedType(RSACredentialsType, Version),
		PublicKeyPEM:      "-----BEGIN PUBLIC KEY-----\nMIIB...",
		PublicKeyPEMFile:  "/path/to/public.pem",
		PrivateKeyPEM:     "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
		PrivateKeyPEMFile: "/path/to/private.pem",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &RSACredentials{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.PublicKeyPEM, restored.PublicKeyPEM)
	assert.Equal(t, original.PublicKeyPEMFile, restored.PublicKeyPEMFile)
	assert.Equal(t, original.PrivateKeyPEM, restored.PrivateKeyPEM)
	assert.Equal(t, original.PrivateKeyPEMFile, restored.PrivateKeyPEMFile)
}
