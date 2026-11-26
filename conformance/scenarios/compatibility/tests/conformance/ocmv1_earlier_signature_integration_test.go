package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	earlierSigAppComponent  = "acme.org/earlier-sig-app"
	earlierSigBaseComponent = "acme.org/earlier-sig-base"
	earlierSigVersion       = "1.0.0"
)

// Test_Integration_OCMv1EarlierSignatureHint covers OCM v1 signing a component
// version with references twice with different hash algorithms. Each v1 signing
// run rewrites the stored reference digests, so OCM v2 can only verify the last
// signature and must explain why the earlier one fails.
func Test_Integration_OCMv1EarlierSignatureHint(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey := filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	constructor := fmt.Sprintf(`components:
- name: %[1]s
  version: %[3]s
  provider:
    name: acme.org
- name: %[2]s
  version: %[3]s
  provider:
    name: acme.org
  componentReferences:
  - name: base
    componentName: %[1]s
    version: %[3]s
`, earlierSigBaseComponent, earlierSigAppComponent, earlierSigVersion)
	constructorPath := filepath.Join(workingDirectory, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))
	ctfPath := filepath.Join(workingDirectory, "v1-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", ctfPath, constructorPath)

	v1Reference := ctfPath + "//" + earlierSigAppComponent + ":" + earlierSigVersion
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "s1", "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "s2", "--private-key", privateKey,
		"--hash", "SHA-512", v1Reference)
	for _, signature := range []string{"s1", "s2"} {
		runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", signature, "--public-key", publicKey, v1Reference)
	}

	configPath := earlierSigConfig(t, publicKey, "s1", "s2")
	reference := "ctf::" + v1Reference

	var descriptors []struct {
		Component struct {
			References []struct {
				Digest map[string]any `json:"digest"`
			} `json:"componentReferences"`
		} `json:"component"`
	}
	r.NoError(json.Unmarshal(runOCMv2Command(t, configPath, "get", "cv", reference, "-o", "json"), &descriptors))
	r.Len(descriptors, 1)
	r.Len(descriptors[0].Component.References, 1)
	r.Equal("SHA-512", descriptors[0].Component.References[0].Digest["hashAlgorithm"], "v1 stores the reference digest of the last signing run")

	runOCMv2Command(t, configPath, "verify", "cv", "--signature", "s2", reference)

	command := exec.CommandContext(t.Context(), os.Getenv("OCM_V2_BIN"),
		"verify", "cv", "--signature", "s1", reference, "--config", configPath, "--loglevel", "error")
	output, err := command.CombinedOutput()
	r.Error(err, "%s", output)
	r.Contains(string(output), "digest mismatch")
	r.Contains(string(output), "possible cause: if this component version was signed several times with OCM v1")
	r.Contains(string(output), `signature "s2" (jsonNormalisation/v3/SHA-512)`)
	r.Contains(string(output), `signature "s1" (jsonNormalisation/v3/SHA-256) cannot be verified by OCM v2`)
}

// earlierSigConfig binds the public key to each named signature, because OCM v2
// resolves verification credentials per signature name.
func earlierSigConfig(t *testing.T, publicKey string, signatures ...string) string {
	t.Helper()

	config := "type: generic.config.ocm.software/v1\nconfigurations:\n- type: credentials.config.ocm.software\n  consumers:\n"
	for _, signature := range signatures {
		config += fmt.Sprintf(`  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: %s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %q
`, signature, publicKey)
	}
	path := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))

	return path
}
