package conformance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const ocmv1Version = "1.0.0-RC1"

type ocmv1Descriptor struct {
	Component struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Resources []struct {
			Name   string         `json:"name"`
			Digest map[string]any `json:"digest"`
			Access map[string]any `json:"access"`
		} `json:"resources"`
	} `json:"component"`
	Signatures []struct {
		Name      string         `json:"name"`
		Digest    map[string]any `json:"digest"`
		Signature map[string]any `json:"signature"`
	} `json:"signatures"`
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

func runOCMv2Command(t *testing.T, configPath string, arguments ...string) []byte {
	t.Helper()
	r := require.New(t)
	binary := os.Getenv("OCM_V2_BIN")
	command := exec.CommandContext(t.Context(), binary, append(arguments, "--config", configPath, "--loglevel", "error")...)
	output, err := command.CombinedOutput()
	r.NoError(err, "%s", output)

	return output
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

func runCompatibilityTool(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s %v: %s", binary, arguments, output)

	return output
}
