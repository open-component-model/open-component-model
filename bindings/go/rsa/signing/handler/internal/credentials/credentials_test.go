package credentials

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
)

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func pkcs1PrivatePEM(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func pkixPublicPEM(key *rsa.PrivateKey) string {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func selfSignedCertPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "creds-*.pem")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestPrivateKeyFromCredentials(t *testing.T) {
	key := newKey(t)
	privPEM := pkcs1PrivatePEM(key)

	t.Run("inline PEM", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{PrivateKeyPEM: privPEM}
		got, err := PrivateKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.D, got.D)
	})

	t.Run("file path", func(t *testing.T) {
		path := writeTempFile(t, privPEM)
		creds := &rsacredentialsv1.RSACredentials{PrivateKeyPEMFile: path}
		got, err := PrivateKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.D, got.D)
	})

	t.Run("empty returns nil", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{}
		got, err := PrivateKeyFromCredentials(creds)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("inline takes priority over file", func(t *testing.T) {
		otherKey := newKey(t)
		path := writeTempFile(t, pkcs1PrivatePEM(otherKey))
		creds := &rsacredentialsv1.RSACredentials{
			PrivateKeyPEM:     privPEM,
			PrivateKeyPEMFile: path,
		}
		got, err := PrivateKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.D, got.D)
	})
}

func TestPublicKeyFromCredentials(t *testing.T) {
	key := newKey(t)
	pubPEM := pkixPublicPEM(key)

	t.Run("inline public key PEM", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{PublicKeyPEM: pubPEM}
		got, err := PublicKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.PublicKey.N, got.PublicKey.N)
	})

	t.Run("file path", func(t *testing.T) {
		path := writeTempFile(t, pubPEM)
		creds := &rsacredentialsv1.RSACredentials{PublicKeyPEMFile: path}
		got, err := PublicKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.PublicKey.N, got.PublicKey.N)
	})

	t.Run("empty with private key derives public", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{PrivateKeyPEM: pkcs1PrivatePEM(key)}
		got, err := PublicKeyFromCredentials(creds)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, key.PublicKey.N, got.PublicKey.N)
	})

	t.Run("completely empty returns nil", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{}
		got, err := PublicKeyFromCredentials(creds)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestCertificateChainFromCredentials(t *testing.T) {
	key := newKey(t)
	certPEM := selfSignedCertPEM(t, key)

	t.Run("inline cert PEM", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{PublicKeyPEM: certPEM}
		got, err := CertificateChainFromCredentials(creds)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "test", got[0].Subject.CommonName)
	})

	t.Run("file path", func(t *testing.T) {
		path := writeTempFile(t, certPEM)
		creds := &rsacredentialsv1.RSACredentials{PublicKeyPEMFile: path}
		got, err := CertificateChainFromCredentials(creds)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "test", got[0].Subject.CommonName)
	})

	t.Run("empty returns nil", func(t *testing.T) {
		creds := &rsacredentialsv1.RSACredentials{}
		got, err := CertificateChainFromCredentials(creds)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestLoadBytes(t *testing.T) {
	t.Run("inline value returned directly", func(t *testing.T) {
		b, err := loadBytes("hello", "")
		require.NoError(t, err)
		assert.Equal(t, []byte("hello"), b)
	})

	t.Run("file read when inline empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data.txt")
		require.NoError(t, os.WriteFile(path, []byte("from-file"), 0o600))
		b, err := loadBytes("", path)
		require.NoError(t, err)
		assert.Equal(t, []byte("from-file"), b)
	})

	t.Run("both empty returns nil", func(t *testing.T) {
		b, err := loadBytes("", "")
		require.NoError(t, err)
		assert.Nil(t, b)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := loadBytes("", filepath.Join(t.TempDir(), "nonexistent.pem"))
		require.Error(t, err)
	})

	t.Run("inline takes priority over file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data.txt")
		require.NoError(t, os.WriteFile(path, []byte("from-file"), 0o600))
		b, err := loadBytes("inline", path)
		require.NoError(t, err)
		assert.Equal(t, []byte("inline"), b)
	})
}
