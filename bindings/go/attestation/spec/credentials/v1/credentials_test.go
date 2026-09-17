package v1_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	ecdsacredentialsv1 "ocm.software/open-component-model/bindings/go/attestation/spec/credentials/v1"
	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
)

func pkcs8PEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: ecdsacredentialsv1.PKCS8PrivateKeyPEMBlockType, Bytes: der})
}

func sec1PEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: ecdsacredentialsv1.ECDSAPrivateKeyPEMBlockType, Bytes: der})
}

func TestParsePrivateKeyPEM_PKCS8AndSEC1(t *testing.T) {
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)

	for name, pemBytes := range map[string][]byte{
		"pkcs8": pkcs8PEM(t, key),
		"sec1":  sec1PEM(t, key),
	} {
		t.Run(name, func(t *testing.T) {
			parsed, err := ecdsacredentialsv1.ParsePrivateKeyPEM(pemBytes)
			require.NoError(t, err)
			require.Equal(t, elliptic.P256(), parsed.Curve)
			require.Zero(t, parsed.D.Cmp(key.D), "parsed key must equal the original")
		})
	}
}

func TestParsePrivateKeyPEM_RejectsNonP256(t *testing.T) {
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	r.NoError(err)
	_, err = ecdsacredentialsv1.ParsePrivateKeyPEM(pkcs8PEM(t, key))
	r.ErrorContains(err, "P-256")
}

func TestParsePrivateKeyPEM_RejectsGarbage(t *testing.T) {
	r := require.New(t)
	_, err := ecdsacredentialsv1.ParsePrivateKeyPEM([]byte("not a pem"))
	r.Error(err)
}

func TestPrivateKeyFromCredentials_InlineAndFile(t *testing.T) {
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)
	keyPEM := pkcs8PEM(t, key)

	t.Run("inline", func(t *testing.T) {
		got, err := ecdsacredentialsv1.PrivateKeyFromCredentials(&ecdsacredentialsv1.ECDSACredentials{PrivateKeyPEM: string(keyPEM)})
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Zero(t, got.D.Cmp(key.D))
	})

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ec.pem")
		require.NoError(t, os.WriteFile(path, keyPEM, 0o600))
		got, err := ecdsacredentialsv1.PrivateKeyFromCredentials(&ecdsacredentialsv1.ECDSACredentials{PrivateKeyPEMFile: path})
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Zero(t, got.D.Cmp(key.D))
	})

	t.Run("inline takes precedence over file", func(t *testing.T) {
		got, err := ecdsacredentialsv1.PrivateKeyFromCredentials(&ecdsacredentialsv1.ECDSACredentials{
			PrivateKeyPEM:     string(keyPEM),
			PrivateKeyPEMFile: "/does/not/exist",
		})
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("empty returns nil", func(t *testing.T) {
		got, err := ecdsacredentialsv1.PrivateKeyFromCredentials(&ecdsacredentialsv1.ECDSACredentials{})
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func TestConvertToECDSACredentials_UntypedDirect(t *testing.T) {
	r := require.New(t)
	// A DirectCredentials property bag with no type, as the credential graph
	// produces on direct resolution, must still yield the key fields.
	got, err := ecdsacredentialsv1.ConvertToECDSACredentials(&directv1.DirectCredentials{
		Properties: map[string]string{"privateKeyPEMFile": "/tmp/key.pem"},
	})
	r.NoError(err)
	r.Equal("/tmp/key.pem", got.PrivateKeyPEMFile)
}
