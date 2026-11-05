package pkitest_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/pkitest"
)

func TestSelfSigned(t *testing.T) {
	r := require.New(t)
	key := pkitest.RSAKey(t)

	cert := pkitest.SelfSigned(t, "signer", key)

	r.Equal("signer", cert.Subject.CommonName)
	r.True(cert.IsCA)
	r.NoError(cert.CheckSignatureFrom(cert))
	r.Equal(&key.PublicKey, cert.PublicKey)
}

func TestIssue_Chain(t *testing.T) {
	r := require.New(t)
	rootKey, leafKey := pkitest.RSAKey(t), pkitest.RSAKey(t)

	root := pkitest.Issue(t, nil, rootKey, "root", true, &rootKey.PublicKey)
	leaf := pkitest.Issue(t, root, rootKey, "leaf", false, &leafKey.PublicKey)

	r.True(root.IsCA)
	r.False(leaf.IsCA)
	r.NoError(leaf.CheckSignatureFrom(root))
	r.NotEqual(root.SerialNumber, leaf.SerialNumber)

	roots := x509.NewCertPool()
	roots.AddCert(root)
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	r.NoError(err)
}

func TestWriteKeyAndChain(t *testing.T) {
	r := require.New(t)
	key := pkitest.RSAKey(t)
	cert := pkitest.SelfSigned(t, "signer", key)
	dir := t.TempDir()

	keyPath, chainPath := pkitest.WriteKeyAndChain(t, dir, key, cert, cert)

	r.Equal(filepath.Join(dir, "key.pem"), keyPath)
	r.Equal(filepath.Join(dir, "chain.pem"), chainPath)

	keyPEM, err := os.ReadFile(keyPath)
	r.NoError(err)
	block, rest := pem.Decode(keyPEM)
	r.Empty(rest)
	r.Equal("RSA PRIVATE KEY", block.Type)
	parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	r.NoError(err)
	r.True(parsed.Equal(key))

	chainPEM, err := os.ReadFile(chainPath)
	r.NoError(err)
	var blocks int
	for block, rest := pem.Decode(chainPEM); block != nil; block, rest = pem.Decode(rest) {
		r.Equal("CERTIFICATE", block.Type)
		r.Equal(cert.Raw, block.Bytes)
		blocks++
	}
	r.Equal(2, blocks)
}

func TestWritePEM(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "x.pem")

	pkitest.WritePEM(t, path, "TEST", []byte{1, 2, 3})

	data, err := os.ReadFile(path)
	r.NoError(err)
	block, _ := pem.Decode(data)
	r.Equal("TEST", block.Type)
	r.Equal([]byte{1, 2, 3}, block.Bytes)
}

func TestClientCert(t *testing.T) {
	r := require.New(t)
	ca, caKey := pkitest.CA(t, "ca")

	certPEM, keyPEM := pkitest.ClientCert(t, ca, caKey, "client")

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	r.NoError(err)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	r.NoError(err)
	r.Equal("client", leaf.Subject.CommonName)
	r.Equal([]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	r.NoError(err)
}
