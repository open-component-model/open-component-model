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

	obj, err := scheme.NewObject(runtime.NewVersionedType(RSACredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(RSACredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)
}

func TestRSACredentials_TypedJSONParsing(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	// Simulates a user-written inline typed credential in .ocmconfig:
	//   type: RSACredentials/v1
	//   privateKeyPEM: "my-key"
	raw := &runtime.Raw{}
	raw.Data = []byte(`{"type":"RSACredentials/v1","privateKeyPEM":"my-key","publicKeyPEMFile":"/path/pub.pem"}`)
	raw.Type = runtime.NewVersionedType(RSACredentialsType, Version)

	creds := &RSACredentials{}
	require.NoError(t, scheme.Convert(raw, creds))

	assert.Equal(t, "my-key", creds.PrivateKeyPEM)
	assert.Equal(t, "/path/pub.pem", creds.PublicKeyPEMFile)
	assert.Empty(t, creds.PublicKeyPEM)
	assert.Empty(t, creds.PrivateKeyPEMFile)
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
