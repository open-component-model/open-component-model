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

const ocmv1Version = "1.0.0-RC1"

type ocmv1Resource struct {
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Type    string           `json:"type"`
	Labels  []map[string]any `json:"labels"`
	Digest  map[string]any   `json:"digest"`
	Access  map[string]any   `json:"access"`
}

type ocmv1Reference struct {
	Name          string           `json:"name"`
	ComponentName string           `json:"componentName"`
	Version       string           `json:"version"`
	Labels        []map[string]any `json:"labels"`
	Digest        map[string]any   `json:"digest"`
}

type ocmv1Signature struct {
	Name      string         `json:"name"`
	Digest    map[string]any `json:"digest"`
	Signature map[string]any `json:"signature"`
}

type ocmv1Descriptor struct {
	Component struct {
		Name       string           `json:"name"`
		Version    string           `json:"version"`
		Labels     []map[string]any `json:"labels"`
		Resources  []ocmv1Resource  `json:"resources"`
		References []ocmv1Reference `json:"componentReferences"`
	} `json:"component"`
	Signatures []ocmv1Signature `json:"signatures"`
}

func newCompatibilityConfig(t *testing.T) string {
	t.Helper()
	r := require.New(t)

	if os.Getenv("OCM_V1_BIN") == "" || os.Getenv("OCM_V2_BIN") == "" {
		t.Skip("run through task conformance/compatibility:run with both CLI binaries")
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	r.NoError(os.WriteFile(path, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))
	return path
}

// newRSAKeyPair creates a bare RSA key pair for the v1 "sign cv --private-key"
// form, which records no issuer. Certificate chains are covered separately.
func newRSAKeyPair(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	privateKey := filepath.Join(directory, "private.pem")
	publicKey := filepath.Join(directory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	return privateKey, publicKey
}

// newVerificationConfig writes an OCM configuration that resolves the public
// key of the given signature, so that "verify cv" finds it.
func newVerificationConfig(t *testing.T, signature, publicKeyPath string) string {
	t.Helper()
	r := require.New(t)

	config := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: %q
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %q
`, signature, publicKeyPath)
	path := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(path, []byte(config), 0o600))

	return path
}

func runOCMv2Command(t *testing.T, configPath string, arguments ...string) []byte {
	t.Helper()
	r := require.New(t)
	output, err := ocmV2Command(t, configPath, arguments...)
	r.NoError(err, "%s", output)

	return output
}

// runFailingOCMv2Command returns the combined output of a command that must
// fail, so a case can assert the message a user sees.
func runFailingOCMv2Command(t *testing.T, configPath string, arguments ...string) string {
	t.Helper()
	r := require.New(t)
	output, err := ocmV2Command(t, configPath, arguments...)
	r.Error(err, "command unexpectedly succeeded: %s", output)

	return string(output)
}

func ocmV2Command(t *testing.T, configPath string, arguments ...string) ([]byte, error) {
	t.Helper()
	binary := os.Getenv("OCM_V2_BIN")
	command := exec.CommandContext(t.Context(), binary, append(arguments, "--config", configPath, "--loglevel", "error")...)

	return command.CombinedOutput()
}

func readCompatDescriptor(t *testing.T, configPath, reference string) ocmv1Descriptor {
	t.Helper()
	r := require.New(t)
	output := runOCMv2Command(t, configPath, "get", "cv", reference, "-o", "json")
	var descriptors []ocmv1Descriptor
	r.NoError(json.Unmarshal(output, &descriptors))
	r.Len(descriptors, 1)

	return descriptors[0]
}

// readOCMv1Descriptor renders a descriptor with the v1 CLI. Cases that assert
// what v2 wrote for a v1 consumer need the v1 view, not the v2 one.
func readOCMv1Descriptor(t *testing.T, configPath, reference string) ocmv1Descriptor {
	t.Helper()
	r := require.New(t)
	output := runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", configPath,
		"get", "cv", reference, "-o", "json")
	// The v1 CLI wraps its listing in an "items" envelope.
	var listing struct {
		Items []ocmv1Descriptor `json:"items"`
	}
	r.NoError(json.Unmarshal(output, &listing))
	r.Len(listing.Items, 1)

	return listing.Items[0]
}

func runCompatibilityTool(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s %v: %s", binary, arguments, output)

	return output
}

// addOCMv1ComponentVersion writes a constructor and builds it into a CTF with
// the v1 CLI. Extra arguments go to "add cv", e.g. --skip-digest-generation.
func addOCMv1ComponentVersion(t *testing.T, v1Config, repository, constructor string, arguments ...string) {
	t.Helper()
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "component-constructor.yaml")
	r.NoError(os.WriteFile(path, []byte(constructor), 0o600))
	arguments = append([]string{"--config", v1Config, "add", "cv", "--create", "--file", repository}, arguments...)
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), append(arguments, path)...)
}
