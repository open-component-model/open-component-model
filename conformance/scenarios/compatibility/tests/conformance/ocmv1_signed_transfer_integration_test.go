package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const ocmv1ComponentName = "ocm.software/ocmv1-compat-signed-basic"

func Test_Integration_OCMv1SignedArchiveTransfer(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	signedPath, publicKey := prepareOCMv1SignedCTF(t, v1Config)

	config := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %q
`, publicKey)
	configPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	signedReference := "ctf::" + signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	signedDescriptor := readCompatDescriptor(t, configPath, signedReference)
	r.Equal(ocmv1ComponentName, signedDescriptor.Component.Name)
	r.Equal(ocmv1Version, signedDescriptor.Component.Version)
	r.Len(signedDescriptor.Signatures, 1)
	runOCMv2Command(t, configPath, "verify", "cv", signedReference)

	archivePath := filepath.Join(t.TempDir(), "signed.tgz")
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", configPath,
		"transfer", "ctf", "--type", "tgz", signedPath, archivePath)
	archiveReference := "ctf::" + archivePath
	r.Equal(signedDescriptor, readCompatDescriptor(t, configPath, archiveReference))

	targetReference := "ctf::" + filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", archiveReference, targetReference)

	signedReference = targetReference + "//" + ocmv1ComponentName + ":" + ocmv1Version
	transferredDescriptor := readCompatDescriptor(t, configPath, signedReference)
	r.Equal(signedDescriptor.Component, transferredDescriptor.Component)
	r.Equal(signedDescriptor.Signatures, transferredDescriptor.Signatures)
	runOCMv2Command(t, configPath, "verify", "cv", signedReference)

	r.Len(signedDescriptor.Component.Resources, 1)

	content := []byte("OCM v1 compatibility fixture")
	path := filepath.Join(t.TempDir(), "text")
	runOCMv2Command(t, configPath, "download", "resource", signedReference,
		"--identity", "name=text", "--output", path, "--extraction-policy", "disable")

	got, err := os.ReadFile(path)
	r.NoError(err)
	r.Equal(content, got)

	hash := sha256.Sum256(content)
	r.Equal(hex.EncodeToString(hash[:]), signedDescriptor.Component.Resources[0].Digest["value"])
}

func prepareOCMv1SignedCTF(t *testing.T, v1Config string) (string, string) {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey := filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	signedConstructor := fmt.Sprintf(`name: %s
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
      text: OCM v1 compatibility fixture
`, ocmv1ComponentName, ocmv1Version, ocmv1Version)
	signedConstructorPath := filepath.Join(workingDirectory, "signed-constructor.yaml")
	r.NoError(os.WriteFile(signedConstructorPath, []byte(signedConstructor), 0o600))
	signedPath := filepath.Join(workingDirectory, "v1-signed-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", signedPath, signedConstructorPath)
	v1Reference := signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default", "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)

	return signedPath, publicKey
}
