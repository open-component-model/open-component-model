package plugin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd/internal/test"
)

// TestDownloadPlugin_ExactMatchWinsOverSubset verifies that when multiple plugin
// resources share the same name+version but differ by extraIdentity (os/architecture),
// an exact identity lookup resolves the correct resource.
func TestDownloadPlugin_ExactMatchWinsOverSubset(t *testing.T) {
	tmp := t.TempDir()

	contentAMD64 := []byte("plugin binary for amd64")
	contentARM64 := []byte("plugin binary for arm64")

	fileAMD64 := filepath.Join(tmp, "plugin-amd64")
	fileARM64 := filepath.Join(tmp, "plugin-arm64")
	require.NoError(t, os.WriteFile(fileAMD64, contentAMD64, 0o755))
	require.NoError(t, os.WriteFile(fileARM64, contentARM64, 0o755))

	// Use a custom plugin type to avoid the default "ocmPlugin" type restriction
	// and --skip-validation to avoid executing the downloaded binary.
	pluginType := "testPlugin"

	constructorYAML := fmt.Sprintf(`
name: ocm.software/test/my-plugin
version: 1.0.0
provider:
  name: test
resources:
  - name: my-plugin
    version: 1.0.0
    type: %s
    extraIdentity:
      os: linux
      architecture: amd64
    input:
      type: file/v1
      path: %s
  - name: my-plugin
    version: 1.0.0
    type: %s
    extraIdentity:
      os: linux
      architecture: arm64
    input:
      type: file/v1
      path: %s
`, pluginType, fileAMD64, pluginType, fileARM64)

	constructorPath := filepath.Join(tmp, "constructor.yaml")
	require.NoError(t, os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

	archivePath := filepath.Join(tmp, "archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorPath,
		"--repository", archivePath,
	))
	require.NoError(t, err, "could not build component version")

	ref := archivePath + "//ocm.software/test/my-plugin:1.0.0"

	t.Run("exact match selects amd64 plugin", func(t *testing.T) {
		outDir := t.TempDir()
		_, err := test.OCM(t, test.WithArgs("download", "plugin", ref,
			"--plugin-type", pluginType,
			"--extra-identity", "os=linux",
			"--extra-identity", "architecture=amd64",
			"--output", outDir,
			"--skip-validation",
		))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(outDir, "my-plugin"))
		require.NoError(t, err)
		require.Equal(t, string(contentAMD64), string(got))
	})

	t.Run("exact match selects arm64 plugin", func(t *testing.T) {
		outDir := t.TempDir()
		_, err := test.OCM(t, test.WithArgs("download", "plugin", ref,
			"--plugin-type", pluginType,
			"--extra-identity", "os=linux",
			"--extra-identity", "architecture=arm64",
			"--output", outDir,
			"--skip-validation",
		))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(outDir, "my-plugin"))
		require.NoError(t, err)
		require.Equal(t, string(contentARM64), string(got))
	})
}
