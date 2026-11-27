package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	nullLabelComponentName = "ocm.software/ocmv1-compat-null-label"
	nullLabelReferenceName = "ocm.software/ocmv1-compat-null-label-ref"
	nullLabelSignatureName = "null-label-signer"
	nullLabelName          = "compat-null"
)

type nullLabelDescriptor struct {
	Component struct {
		Labels              []map[string]any `json:"labels"`
		ComponentReferences []struct {
			Labels []map[string]any `json:"labels"`
		} `json:"componentReferences"`
	} `json:"component"`
	Signatures []struct {
		Digest struct {
			HashAlgorithm          string `json:"hashAlgorithm"`
			NormalisationAlgorithm string `json:"normalisationAlgorithm"`
			Value                  string `json:"value"`
		} `json:"digest"`
	} `json:"signatures"`
}

// Test_Integration_OCMv1NullLabelSignature covers an OCM v1 jsonNormalisation/v3
// signature over signed labels with value: null on the component and on a
// component reference. OCM v1 keeps "value":null in the normalised bytes, so
// OCM v2 must keep it to reproduce the signed digest and a v3 digest pin.
func Test_Integration_OCMv1NullLabelSignature(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	signedPath, publicKey := prepareNullLabelSignedCTF(t, v1Config)

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
`, nullLabelSignatureName, publicKey)
	configPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	reference := "ctf::" + signedPath + "//" + nullLabelComponentName + ":" + ocmv1Version
	output := runOCMv2Command(t, configPath, "get", "cv", reference, "-o", "json")
	var descriptors []nullLabelDescriptor
	r.NoError(json.Unmarshal(output, &descriptors))
	r.Len(descriptors, 1)
	descriptor := descriptors[0]

	r.Len(descriptor.Component.ComponentReferences, 1)
	for _, labels := range [][]map[string]any{descriptor.Component.Labels, descriptor.Component.ComponentReferences[0].Labels} {
		r.Equal([]map[string]any{{"name": nullLabelName, "value": nil, "signing": true}}, labels)
	}

	r.Len(descriptor.Signatures, 1)
	digest := descriptor.Signatures[0].Digest
	r.Equal("jsonNormalisation/v3", digest.NormalisationAlgorithm)
	r.Equal("SHA-256", digest.HashAlgorithm)

	runOCMv2Command(t, configPath, "verify", "cv", reference)

	// A pin carries the algorithm in OCI digest grammar, so "/" becomes ".".
	pin := strings.ReplaceAll(digest.NormalisationAlgorithm, "/", ".") + "+sha256:" + digest.Value
	runOCMv2Command(t, configPath, "get", "cv", reference+"@"+pin)
}

// prepareNullLabelSignedCTF builds a component version with OCM v1 whose own
// label and reference label are signed with value: null, and signs it with
// jsonNormalisation/v3. It returns the CTF directory and the public key.
func prepareNullLabelSignedCTF(t *testing.T, v1Config string) (string, string) {
	t.Helper()
	r := require.New(t)

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
    name: ocm.software
- name: %[2]s
  version: %[3]s
  provider:
    name: ocm.software
  labels:
  - name: %[4]s
    value: null
    signing: true
  componentReferences:
  - name: ref
    componentName: %[1]s
    version: %[3]s
    labels:
    - name: %[4]s
      value: null
      signing: true
`, nullLabelReferenceName, nullLabelComponentName, ocmv1Version, nullLabelName)
	constructorPath := filepath.Join(workingDirectory, "null-label-constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

	signedPath := filepath.Join(workingDirectory, "v1-null-label-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", signedPath, constructorPath)

	v1Reference := signedPath + "//" + nullLabelComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv",
		"--signature", nullLabelSignatureName, "--normalization", "jsonNormalisation/v3",
		"--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv",
		"--signature", nullLabelSignatureName, "--public-key", publicKey, v1Reference)

	return signedPath, publicKey
}
