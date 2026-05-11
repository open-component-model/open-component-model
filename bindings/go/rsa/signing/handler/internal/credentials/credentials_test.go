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
	privFile := writeTempFile(t, privPEM)
	otherFile := writeTempFile(t, pkcs1PrivatePEM(newKey(t)))

	tests := []struct {
		name    string
		creds   *rsacredentialsv1.RSACredentials
		wantNil bool
	}{
		{
			name:  "inline PEM",
			creds: &rsacredentialsv1.RSACredentials{PrivateKeyPEM: privPEM},
		},
		{
			name:  "file path",
			creds: &rsacredentialsv1.RSACredentials{PrivateKeyPEMFile: privFile},
		},
		{
			name:    "empty returns nil",
			creds:   &rsacredentialsv1.RSACredentials{},
			wantNil: true,
		},
		{
			name: "inline takes priority over file",
			creds: &rsacredentialsv1.RSACredentials{
				PrivateKeyPEM:     privPEM,
				PrivateKeyPEMFile: otherFile,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PrivateKeyFromCredentials(tt.creds)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, key.D, got.D)
		})
	}
}

func TestPublicKeyFromCredentials(t *testing.T) {
	key := newKey(t)
	pubPEM := pkixPublicPEM(key)
	pubFile := writeTempFile(t, pubPEM)

	tests := []struct {
		name    string
		creds   *rsacredentialsv1.RSACredentials
		wantNil bool
	}{
		{
			name:  "inline public key PEM",
			creds: &rsacredentialsv1.RSACredentials{PublicKeyPEM: pubPEM},
		},
		{
			name:  "file path",
			creds: &rsacredentialsv1.RSACredentials{PublicKeyPEMFile: pubFile},
		},
		{
			name:  "empty with private key derives public",
			creds: &rsacredentialsv1.RSACredentials{PrivateKeyPEM: pkcs1PrivatePEM(key)},
		},
		{
			name:    "completely empty returns nil",
			creds:   &rsacredentialsv1.RSACredentials{},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PublicKeyFromCredentials(tt.creds)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, key.PublicKey.N, got.PublicKey.N)
		})
	}
}

func TestCertificateChainFromCredentials(t *testing.T) {
	key := newKey(t)
	certPEM := selfSignedCertPEM(t, key)
	certFile := writeTempFile(t, certPEM)

	tests := []struct {
		name    string
		creds   *rsacredentialsv1.RSACredentials
		wantNil bool
	}{
		{
			name:  "inline cert PEM",
			creds: &rsacredentialsv1.RSACredentials{PublicKeyPEM: certPEM},
		},
		{
			name:  "file path",
			creds: &rsacredentialsv1.RSACredentials{PublicKeyPEMFile: certFile},
		},
		{
			name:    "empty returns nil",
			creds:   &rsacredentialsv1.RSACredentials{},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CertificateChainFromCredentials(tt.creds)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, "test", got[0].Subject.CommonName)
		})
	}
}

func TestLoadBytes(t *testing.T) {
	existingFile := filepath.Join(t.TempDir(), "data.txt")
	require.NoError(t, os.WriteFile(existingFile, []byte("from-file"), 0o600))
	missingFile := filepath.Join(t.TempDir(), "nonexistent.pem")

	tests := []struct {
		name    string
		inline  string
		file    string
		want    []byte
		wantErr bool
	}{
		{name: "inline value returned directly", inline: "hello", want: []byte("hello")},
		{name: "file read when inline empty", file: existingFile, want: []byte("from-file")},
		{name: "both empty returns nil"},
		{name: "missing file returns error", file: missingFile, wantErr: true},
		{name: "inline takes priority over file", inline: "inline", file: existingFile, want: []byte("inline")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := loadBytes(tt.inline, tt.file)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, b)
		})
	}
}
