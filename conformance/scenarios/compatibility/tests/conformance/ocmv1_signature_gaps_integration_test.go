package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Known gaps in how OCM v2 verifies signatures that OCM v1 created.
// Every case is linked to its issue with knownGap and asserts the
// behaviour v2 shows today, so the fix turns it into a regular case.

const (
	ocmv1CertificateComponentName = "ocm.software/ocmv1-compat-certificate"
	ocmv1NullLabelComponentName   = "ocm.software/ocmv1-compat-null-label"
	ocmv1LegacyNormComponentName  = "ocm.software/ocmv1-compat-legacy-normalisation"
)

// Test_Integration_OCMv1CertificateSignature covers a v1 signature made with a
// certificate chain. v1 records the subject of the signing certificate in
// signature.issuer; v2 compares it with the issuer DN of the leaf.
func Test_Integration_OCMv1CertificateSignature(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1367)

	v1Config := newCompatibilityConfig(t)
	signature := "repro-signer"
	privateKey, leafCertificate, rootCertificate := newOCMv1CertificateChain(t, signature)

	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, plainTextConstructor(ocmv1CertificateComponentName))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1CertificateComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", signature,
		"--private-key", privateKey, "--public-key", leafCertificate, "--ca-cert", rootCertificate, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", signature,
		"--ca-cert", rootCertificate, v1Reference)

	configPath := newVerificationConfig(t, signature, rootCertificate)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal("CN="+signature, descriptor.Signatures[0].Signature["issuer"],
		"v1 records the subject of the signing certificate")

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "issuer mismatch")
}

// Test_Integration_OCMv1NullLabelSignature covers a signing-relevant label with
// value: null. The JCS normalisation engine drops nil map entries, so the
// normalised descriptor differs from the one v1 signed.
func Test_Integration_OCMv1NullLabelSignature(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1402)

	v1Config := newCompatibilityConfig(t)
	privateKey, publicKey := newRSAKeyPair(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: null-label
    value: null
    signing: true
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 null label fixture
`, ocmv1NullLabelComponentName, ocmv1Version, ocmv1Version))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1NullLabelComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName, "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", ocmv1SignatureName, "--public-key", publicKey, v1Reference)

	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Equal([]map[string]any{{"name": "null-label", "value": nil, "signing": true}}, descriptor.Component.Labels,
		"v2 reads the null value; only normalisation drops it")

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "digest mismatch")
}

// Test_Integration_OCMv1LegacyNormalisation covers jsonNormalisation/v2, the
// only v1 normalisation algorithm without a v2 mapping.
func Test_Integration_OCMv1LegacyNormalisation(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1403)

	v1Config := newCompatibilityConfig(t)
	privateKey, publicKey := newRSAKeyPair(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, plainTextConstructor(ocmv1LegacyNormComponentName))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1LegacyNormComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName,
		"--private-key", privateKey, "--normalization", "jsonNormalisation/v2", v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", ocmv1SignatureName, "--public-key", publicKey, v1Reference)

	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal("jsonNormalisation/v2", descriptor.Signatures[0].Digest["normalisationAlgorithm"])

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "unknown normalisation algorithm: jsonNormalisation/v2")
}

// newOCMv1CertificateChain creates a self-signed root and a code-signing leaf
// certificate whose common name is the signature name, which is what v1 writes
// into signature.issuer by default.
func newOCMv1CertificateChain(t *testing.T, commonName string) (string, string, string) {
	t.Helper()
	r := require.New(t)

	directory := t.TempDir()
	rootKey := filepath.Join(directory, "root.key")
	rootCertificate := filepath.Join(directory, "root.pem")
	privateKey := filepath.Join(directory, "leaf.key")
	request := filepath.Join(directory, "leaf.csr")
	leafCertificate := filepath.Join(directory, "leaf.pem")
	extensions := filepath.Join(directory, "leaf.ext")

	runCompatibilityTool(t, "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", rootKey, "-out", rootCertificate, "-days", "1", "-subj", "/CN=ocm-compat-root")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "req", "-new", "-key", privateKey, "-out", request, "-subj", "/CN="+commonName)
	r.NoError(os.WriteFile(extensions,
		[]byte("basicConstraints=CA:FALSE\nkeyUsage=digitalSignature\nextendedKeyUsage=codeSigning\n"), 0o600))
	runCompatibilityTool(t, "openssl", "x509", "-req", "-in", request, "-CA", rootCertificate, "-CAkey", rootKey,
		"-out", leafCertificate, "-days", "1", "-set_serial", "1", "-extfile", extensions)

	return privateKey, leafCertificate, rootCertificate
}
