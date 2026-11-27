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
	refVerifyParentName = "ocm.software/ocmv1-compat-ref-verify-parent"
	refVerifyChildName  = "ocm.software/ocmv1-compat-ref-verify-child"
)

// Test_Integration_OCMv1ReferenceVerification covers a parent component
// version signed by OCM v1 that references a child in the same CTF. OCM v1
// stores the child's descriptor digest in the reference and rejects the
// signature when the child is missing or no longer matches that digest.
func Test_Integration_OCMv1ReferenceVerification(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	v1Binary := os.Getenv("OCM_V1_BIN")
	signedPath, publicKey := refVerifyPrepareSignedCTF(t, v1Config)

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

	t.Run("referenced child present and unchanged", func(t *testing.T) {
		r := require.New(t)
		descriptor := readCompatDescriptor(t, configPath, refVerifyParentReference("ctf::"+signedPath))
		r.Len(descriptor.Signatures, 1)

		output, err := refVerifyRunOCM(t, os.Getenv("OCM_V2_BIN"), "verify", "cv", refVerifyParentReference("ctf::"+signedPath),
			"--config", configPath, "--loglevel", "debug")
		r.NoError(err, "%s", output)
		r.Contains(output, `msg="verified component reference" reference=child component=`+refVerifyChildName)
	})

	tests := []struct {
		name     string
		prepare  func(t *testing.T) string
		expected string
	}{
		{
			name: "referenced child missing",
			prepare: func(t *testing.T) string {
				t.Helper()
				return refVerifyTransferParentOnly(t, v1Config, signedPath)
			},
			expected: "failed resolving component reference child[" + refVerifyChildName + ":" + ocmv1Version + "]",
		},
		{
			name: "referenced child changed after signing",
			prepare: func(t *testing.T) string {
				t.Helper()
				target := refVerifyTransferParentOnly(t, v1Config, signedPath)
				constructor := refVerifyWriteConstructor(t, refVerifyChildConstructor("changed child content"))
				runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--file", target, constructor)
				return target
			},
			expected: "component version " + refVerifyChildName + ":" + ocmv1Version + " referenced by " +
				refVerifyParentName + ":" + ocmv1Version + " does not match the digest stored in the reference",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			target := tc.prepare(t)

			// OCM v1 itself rejects the CTF, so OCM v2 must do so as well.
			output, err := refVerifyRunOCM(t, v1Binary, "--config", v1Config, "verify", "cv",
				"--signature", "default", "--public-key", publicKey, refVerifyParentReference(target))
			r.Error(err, "OCM v1 accepted the CTF: %s", output)

			output, err = refVerifyRunOCM(t, os.Getenv("OCM_V2_BIN"), "verify", "cv", refVerifyParentReference("ctf::"+target),
				"--config", configPath, "--loglevel", "error")
			r.Error(err, "OCM v2 accepted the CTF: %s", output)
			r.Contains(output, "REFERENCE VERIFICATION FAILED")
			r.Contains(output, tc.expected)
		})
	}
}

// refVerifyPrepareSignedCTF builds a parent and its referenced child with OCM v1
// and signs only the parent; OCM v1 digests the child into the reference.
func refVerifyPrepareSignedCTF(t *testing.T, v1Config string) (string, string) {
	t.Helper()

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey := filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	childConstructor := refVerifyWriteConstructor(t, refVerifyChildConstructor("child content"))
	parentConstructor := refVerifyWriteConstructor(t, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
componentReferences:
- name: child
  componentName: %s
  version: %s
`, refVerifyParentName, ocmv1Version, refVerifyChildName, ocmv1Version))
	signedPath := filepath.Join(workingDirectory, "v1-signed-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", signedPath, childConstructor, parentConstructor)
	v1Reference := refVerifyParentReference(signedPath)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default", "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)

	return signedPath, publicKey
}

// refVerifyTransferParentOnly copies the signed parent without its reference,
// which is the default (non-recursive) OCM v1 transfer.
func refVerifyTransferParentOnly(t *testing.T, v1Config, signedPath string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "parent-only-ctf")
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"transfer", "cv", refVerifyParentReference(signedPath), target)
	return target
}

func refVerifyChildConstructor(text string) string {
	return fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
- name: text
  type: plainText
  relation: local
  input:
    type: utf8
    text: %s
`, refVerifyChildName, ocmv1Version, text)
}

func refVerifyWriteConstructor(t *testing.T, constructor string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "component-constructor.yaml")
	require.NoError(t, os.WriteFile(path, []byte(constructor), 0o600))
	return path
}

func refVerifyParentReference(repository string) string {
	return repository + "//" + refVerifyParentName + ":" + ocmv1Version
}

// refVerifyRunOCM returns the output and error of a CLI call that may be expected to fail.
func refVerifyRunOCM(t *testing.T, binary string, arguments ...string) (string, error) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), binary, arguments...).CombinedOutput()
	return string(output), err
}
