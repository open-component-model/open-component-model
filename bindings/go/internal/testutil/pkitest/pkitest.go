// Package pkitest creates throwaway keys and certificates for tests.
//
// The RSA helpers serve the signing tests: a key, a self-signed certificate or a chain, and
// PEM files on disk:
//
//	key := pkitest.RSAKey(t)
//	ca := pkitest.SelfSigned(t, "test-ca", key)
//	leafKey := pkitest.RSAKey(t)
//	leaf := pkitest.Issue(t, ca, key, "leaf", false, &leafKey.PublicKey)
//	keyPath, chainPath := pkitest.WriteKeyAndChain(t, t.TempDir(), leafKey, leaf, ca)
//	caPath := pkitest.WriteCerts(t, t.TempDir(), "ca.pem", ca)
//
// [WritePEM] writes one DER block as a PEM file for the cases the other writers do not cover.
//
// The ECDSA helpers serve the TLS tests: a CA and a client certificate for mTLS, both as PEM:
//
//	caCert, caKey := pkitest.CA(t, "test-ca")
//	certPEM, keyPEM := pkitest.ClientCert(t, caCert, caKey, "client")
package pkitest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
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

	"github.com/stretchr/testify/require"
)

const (
	rsaBits   = 2048
	serialLen = 128

	// PEM block types.
	typeCertificate   = "CERTIFICATE"
	typeRSAPrivateKey = "RSA PRIVATE KEY"
	typePrivateKey    = "PRIVATE KEY"
)

// RSAKey returns a new 2048-bit RSA key.
func RSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, rsaBits)
	require.NoError(t, err)
	return key
}

// SerialNumber returns a random 128-bit certificate serial number.
func SerialNumber(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialLen))
	require.NoError(t, err)
	return n
}

// SelfSigned returns a self-signed CA certificate for cn, signed with key. It became valid an
// hour ago and expires in a day.
func SelfSigned(t *testing.T, cn string, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          SerialNumber(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	return create(t, tmpl, tmpl, &key.PublicKey, key)
}

// Issue returns a certificate for cn and the public key pub, signed by parent with parentKey.
// A nil parent makes it self-signed. It became valid an hour ago and expires in a week.
func Issue(t *testing.T, parent *x509.Certificate, parentKey *rsa.PrivateKey, cn string, isCA bool, pub *rsa.PublicKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          SerialNumber(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: isCA,
		IsCA:                  isCA,
	}
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	if parent == nil {
		parent = tmpl
	}
	return create(t, tmpl, parent, pub, parentKey)
}

// WritePEM writes der as one PEM block of type typ to path.
func WritePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
}

// WriteCerts writes certs as CERTIFICATE PEM blocks, in the given order, to dir/name and
// returns that path.
func WriteCerts(t *testing.T, dir, name string, certs ...*x509.Certificate) string {
	t.Helper()
	var out []byte
	for _, cert := range certs {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: typeCertificate, Bytes: cert.Raw})...)
	}
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, out, 0o600))
	return path
}

// WriteKeyAndChain writes key as PKCS #1 PEM to dir/key.pem and chain to dir/chain.pem and
// returns both paths.
func WriteKeyAndChain(t *testing.T, dir string, key *rsa.PrivateKey, chain ...*x509.Certificate) (keyPath, chainPath string) {
	t.Helper()
	keyPath = filepath.Join(dir, "key.pem")
	WritePEM(t, keyPath, typeRSAPrivateKey, x509.MarshalPKCS1PrivateKey(key))
	chainPath = WriteCerts(t, dir, "chain.pem", chain...)
	return keyPath, chainPath
}

// CA returns a self-signed P-256 CA certificate for cn and its key, valid for an hour either
// side of now.
func CA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          SerialNumber(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	return create(t, tmpl, tmpl, &key.PublicKey, key), key
}

// ClientCert returns a PEM-encoded P-256 client-auth certificate for cn, signed by ca with
// caKey, and its PKCS #8 PEM key.
func ClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: SerialNumber(t),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	cert := create(t, tmpl, ca, &key.PublicKey, caKey)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: typeCertificate, Bytes: cert.Raw})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: typePrivateKey, Bytes: keyDER})
	return certPEM, keyPEM
}

func create(t *testing.T, tmpl, parent *x509.Certificate, pub, priv any) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, priv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}
