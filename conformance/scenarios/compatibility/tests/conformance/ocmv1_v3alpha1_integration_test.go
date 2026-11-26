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
	ocmv1V3Alpha1ComponentName = "ocm.software/ocmv1-compat-v3alpha1"
	ocmv1V3Alpha1Schema        = "ocm.software/v3alpha1"
)

// Test_Integration_OCMv1V3Alpha1Descriptor covers CTFs that OCM v1 writes with
// --scheme ocm.software/v3alpha1. v2 cannot read that serialization, so it must fail with
// an error naming the schema instead of decoding an empty descriptor.
func Test_Integration_OCMv1V3Alpha1Descriptor(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	ctfPath := prepareOCMv1V3Alpha1CTF(t, configPath)
	reference := "ctf::" + ctfPath + "//" + ocmv1V3Alpha1ComponentName + ":" + ocmv1Version

	// runOCMv2Command requires success, so the expected failure runs inline.
	command := exec.CommandContext(t.Context(), os.Getenv("OCM_V2_BIN"),
		"get", "cv", reference, "--config", configPath, "--loglevel", "error")
	output, err := command.CombinedOutput()
	r.Error(err, "%s", output)
	// Only the schema name is asserted; the remaining wording may change once ocm-spec deprecates it.
	r.Contains(string(output), ocmv1V3Alpha1Schema)
}

func prepareOCMv1V3Alpha1CTF(t *testing.T, v1Config string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	constructor := fmt.Sprintf("name: %s\nversion: %s\nprovider:\n  name: ocm.software\n",
		ocmv1V3Alpha1ComponentName, ocmv1Version)
	constructorPath := filepath.Join(workingDirectory, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))
	ctfPath := filepath.Join(workingDirectory, "v1-v3alpha1-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", ctfPath,
		"--scheme", ocmv1V3Alpha1Schema, constructorPath)

	// v1 reads the version back, proving the fixture really carries the v3alpha1 serialization.
	stored := runCompatibilityTool(t, v1Binary, "--config", v1Config, "get", "cv", ctfPath, "-o", "yaml")
	r.Contains(string(stored), "apiVersion: "+ocmv1V3Alpha1Schema)

	return ctfPath
}
