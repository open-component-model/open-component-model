package conformance

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1CertificateComponentName = "ocm.software/ocmv1-compat-signed-certificate"
	ocmv1CertificateSignatureName = "compat-signer"
)

// Test_Integration_OCMv1CertificateSignature covers an OCM v1 signature created
// with a certificate chain. OCM v1 records the subject of the signing
// certificate in the signature issuer field, not the issuer of that certificate.
func Test_Integration_OCMv1CertificateSignature(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	signedPath, rootCertificate := prepareOCMv1CertificateSignedCTF(t, v1Config)

	config := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: %s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %q
`, ocmv1CertificateSignatureName, rootCertificate)
	configPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	reference := "ctf::" + signedPath + "//" + ocmv1CertificateComponentName + ":" + ocmv1Version
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal(ocmv1CertificateSignatureName, descriptor.Signatures[0].Name)
	r.Equal("CN="+ocmv1CertificateSignatureName, descriptor.Signatures[0].Signature["issuer"])

	runOCMv2Command(t, configPath, "verify", "cv", reference)
}

// prepareOCMv1CertificateSignedCTF builds a component version with OCM v1 and
// signs it with a leaf certificate issued by a root CA. It returns the CTF
// directory and the root certificate that verifies the embedded chain.
func prepareOCMv1CertificateSignedCTF(t *testing.T, v1Config string) (string, string) {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")

	rootKey, rootCertificate := issueCompatCertificate(t, nil, nil, "compat-root", true)
	leafKey, leafCertificate := issueCompatCertificate(t, rootCertificate, rootKey, ocmv1CertificateSignatureName, false)

	privateKeyPath := filepath.Join(workingDirectory, "leaf.key")
	writeCompatPEM(t, privateKeyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))
	leafPath := filepath.Join(workingDirectory, "leaf.pem")
	writeCompatPEM(t, leafPath, "CERTIFICATE", leafCertificate.Raw)
	rootPath := filepath.Join(workingDirectory, "root.pem")
	writeCompatPEM(t, rootPath, "CERTIFICATE", rootCertificate.Raw)

	constructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 certificate compatibility fixture
`, ocmv1CertificateComponentName, ocmv1Version, ocmv1Version)
	constructorPath := filepath.Join(workingDirectory, "certificate-constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

	signedPath := filepath.Join(workingDirectory, "v1-certificate-signed-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", signedPath, constructorPath)

	v1Reference := signedPath + "//" + ocmv1CertificateComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv",
		"--signature", ocmv1CertificateSignatureName,
		"--private-key", privateKeyPath, "--public-key", leafPath, "--ca-cert", rootPath, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv",
		"--signature", ocmv1CertificateSignatureName, "--ca-cert", rootPath, v1Reference)

	return signedPath, rootPath
}

// issueCompatCertificate returns a key and a certificate for commonName. With a
// nil parent the certificate is self-signed. Leaf certificates carry the code
// signing usages OCM v1 and v2 both require.
func issueCompatCertificate(t *testing.T, parent *x509.Certificate, parentKey *rsa.PrivateKey, commonName string, isCA bool) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	r := require.New(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	if parent == nil {
		parent, parentKey = template, key
	}

	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	r.NoError(err)
	certificate, err := x509.ParseCertificate(der)
	r.NoError(err)

	return key, certificate
}

func writeCompatPEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600))
}
