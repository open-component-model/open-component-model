package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	props := map[string]string{
		CredentialKeyPublicKeyPEM:      "test-public-key",
		CredentialKeyPublicKeyPEMFile:  "/path/to/public.pem",
		CredentialKeyPrivateKeyPEM:     "test-private-key",
		CredentialKeyPrivateKeyPEMFile: "/path/to/private.pem",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "test-public-key", creds.PublicKeyPEM)
	assert.Equal(t, "/path/to/public.pem", creds.PublicKeyPEMFile)
	assert.Equal(t, "test-private-key", creds.PrivateKeyPEM)
	assert.Equal(t, "/path/to/private.pem", creds.PrivateKeyPEMFile)
	assert.Equal(t, runtime.NewVersionedType(RSACredentialsType, Version), creds.Type)
}

func TestFromDirectCredentials_PartialFields(t *testing.T) {
	props := map[string]string{
		CredentialKeyPrivateKeyPEM: "test-private-key",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "test-private-key", creds.PrivateKeyPEM)
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
		PublicKeyPEM:      "test-public-key",
		PublicKeyPEMFile:  "/path/to/public.pem",
		PrivateKeyPEM:     "test-private-key",
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
