package conformance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const ocmv1LegacyNormalisation = "jsonNormalisation/v1"

// Test_Integration_OCMv1JSONNormalisationV1Signature covers signatures OCM v1
// creates with jsonNormalisation/v1. Its digest differs from the cc-utils one
// for the Helm chart type and for labels with decimal numbers or HTML
// characters, so OCM v2 verifies these through the OCM v1 variant; the source
// reference must not stop that. A signed label holding objects inside an array
// is refused with a re-sign hint.
func Test_Integration_OCMv1JSONNormalisationV1Signature(t *testing.T) {
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
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	for _, tc := range []struct {
		name    string
		label   string
		wantErr string
	}{
		{name: "ocmv1-variant", label: `[{name: number, value: 0.00001, signing: true}, {name: html, value: "a<b>&c", signing: true}]`},
		{name: "objects-in-array", label: `[{name: list, value: [{b: 1, a: 2}], signing: true}]`, wantErr: "rewrites objects inside arrays"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			component := "ocm.software/ocmv1-compat-jsonnormalisation-v1-" + tc.name
			reference := "ctf::" + prepareOCMv1JSONNormalisationV1CTF(t, v1Config, privateKey, publicKey, component, tc.label) +
				"//" + component + ":" + ocmv1Version

			descriptor := readCompatDescriptor(t, configPath, reference)
			r.Len(descriptor.Signatures, 1)
			r.Equal(ocmv1LegacyNormalisation, descriptor.Signatures[0].Digest["normalisationAlgorithm"])

			if tc.wantErr == "" {
				runOCMv2Command(t, configPath, "verify", "cv", reference)
				return
			}
			command := exec.CommandContext(t.Context(), os.Getenv("OCM_V2_BIN"), "verify", "cv", reference, "--config", configPath, "--loglevel", "error")
			output, err := command.CombinedOutput()
			r.Error(err, "%s", output)
			r.Contains(string(output), tc.wantErr)
			r.Contains(string(output), "re-sign the component version")
		})
	}
}

// prepareOCMv1JSONNormalisationV1CTF builds a component version with OCM v1
// holding a Helm chart resource with a source reference and the given
// component labels, signs it with jsonNormalisation/v1 and verifies it with
// OCM v1. It returns the CTF directory.
func prepareOCMv1JSONNormalisationV1CTF(t *testing.T, v1Config, privateKey, publicKey, component, labels string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")

	constructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels: %s
sources:
  - name: source
    version: %s
    type: git
    access:
      type: gitHub
      repoUrl: github.com/open-component-model/open-component-model
      commit: "0000000000000000000000000000000000000000"
resources:
  - name: chart
    version: %s
    type: helmChart
    relation: local
    srcRefs:
      - identitySelector:
          name: source
    input:
      type: utf8
      text: OCM v1 jsonNormalisation/v1 compatibility fixture
`, component, ocmv1Version, labels, ocmv1Version, ocmv1Version)
	constructorPath := filepath.Join(workingDirectory, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

	path := filepath.Join(workingDirectory, "v1-jsonnormalisation-v1-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", path, constructorPath)

	v1Reference := path + "//" + component + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default",
		"--normalization", ocmv1LegacyNormalisation, "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)

	return path
}
