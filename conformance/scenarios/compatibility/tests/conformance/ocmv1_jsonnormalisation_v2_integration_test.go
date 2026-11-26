package conformance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1NormalisationV2Algorithm     = "jsonNormalisation/v2"
	ocmv1NormalisationV2ComponentName = "ocm.software/ocmv1-compat-jsonnormalisation-v2"
)

// Test_Integration_OCMv1JSONNormalisationV2Rejected covers signatures OCM v1
// creates with jsonNormalisation/v2. OCM v2 implements neither verifying nor
// creating that algorithm, so verify must name it and tell the user to re-sign.
func Test_Integration_OCMv1JSONNormalisationV2Rejected(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)

	workingDirectory := t.TempDir()
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey := filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

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
	configPath := filepath.Join(workingDirectory, "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	reference := "ctf::" + prepareOCMv1JSONNormV2CTF(t, v1Config, privateKey, publicKey) +
		"//" + ocmv1NormalisationV2ComponentName + ":" + ocmv1Version

	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal(ocmv1NormalisationV2Algorithm, descriptor.Signatures[0].Digest["normalisationAlgorithm"])

	command := exec.CommandContext(t.Context(), os.Getenv("OCM_V2_BIN"), "verify", "cv", reference, "--config", configPath, "--loglevel", "error")
	output, err := command.CombinedOutput()
	r.Error(err, "%s", output)
	r.Contains(string(output), `"`+ocmv1NormalisationV2Algorithm+`" is deprecated`)
	r.Contains(string(output), "re-sign the component version")
}

// prepareOCMv1JSONNormV2CTF builds a component version with OCM v1, signs it
// with jsonNormalisation/v2 and verifies it with OCM v1. It returns the CTF
// directory.
func prepareOCMv1JSONNormV2CTF(t *testing.T, v1Config, privateKey, publicKey string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")

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
      text: OCM v1 jsonNormalisation/v2 compatibility fixture
`, ocmv1NormalisationV2ComponentName, ocmv1Version, ocmv1Version)
	constructorPath := filepath.Join(workingDirectory, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

	path := filepath.Join(workingDirectory, "v1-jsonnormalisation-v2-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", path, constructorPath)

	v1Reference := path + "//" + ocmv1NormalisationV2ComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default",
		"--normalization", ocmv1NormalisationV2Algorithm, "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)

	return path
}
