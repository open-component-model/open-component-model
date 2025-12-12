package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
)

func TestDownloadPluginIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "ecrplugin")

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0",
		"--extra-identity", "os=linux",
		"--extra-identity", "architecture=amd64",
		"--output", outputPath,
		"--skip-validation",
		"--config", cfgPath,
	})
	require.NoError(t, downloadCMD.ExecuteContext(t.Context()), "DownloadPlugin should succeed")
	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	assert.FileExists(t, pluginLocation, "binary should be downloaded")

	info, err := os.Stat(pluginLocation)
	require.NoError(t, err, "should be able to stat the downloaded file")
	assert.True(t, info.Mode().IsRegular(), "downloaded file should be a regular file")
	assert.Greater(t, info.Size(), int64(0), "downloaded file should not be empty")
}

func TestDownloadPluginIntegrationWithResolvers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: OCIRepository
      hostname: ghcr.io
    componentNamePattern: ghcr.io
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir)

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0",
		"--extra-identity", "os=linux",
		"--extra-identity", "architecture=amd64",
		"--output", outputPath,
		"--skip-validation",
		"--config", cfgPath,
	})

	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	require.NoError(t, downloadCMD.ExecuteContext(t.Context()), "DownloadPlugin should succeed")
	assert.FileExists(t, pluginLocation, "binary should be downloaded")

	info, err := os.Stat(pluginLocation)
	require.NoError(t, err, "should be able to stat the downloaded file")
	assert.True(t, info.Mode().IsRegular(), "downloaded file should be a regular file")
	assert.Greater(t, info.Size(), int64(0), "downloaded file should not be empty")
}

func TestDownloadPluginIntegrationWithFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: ocm.config.ocm.software
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      hostname: ghcr.io
- type: credentials.config.ocm.software
  consdumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir)

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0",
		"--extra-identity", "os=linux",
		"--extra-identity", "architecture=amd64",
		"--output", outputPath,
		"--skip-validation",
		"--config", cfgPath,
	})
	require.NoError(t, downloadCMD.ExecuteContext(t.Context()), "DownloadPlugin should succeed")

	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	assert.FileExists(t, pluginLocation, "binary should be downloaded")

	info, err := os.Stat(pluginLocation)
	require.NoError(t, err, "should be able to stat the downloaded file")
	assert.True(t, info.Mode().IsRegular(), "downloaded file should be a regular file")
	assert.Greater(t, info.Size(), int64(0), "downloaded file should not be empty")
}

func TestDownloadPluginMissingResourceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir)

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0",
		"--extra-identity", "os=does-not-exist",
		"--extra-identity", "architecture=amd64",
		"--output", outputPath,
		"--skip-validation",
		"--config", cfgPath,
	})
	err := downloadCMD.ExecuteContext(t.Context())
	require.Error(t, err, "DownloadPlugin should fail for non-existent resource")
	assert.Contains(t, err.Error(), "no resource found matching identity", "error should mention missing resource")

	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	assert.NoFileExists(t, pluginLocation, "plugin binary should not be downloaded for non-existent resource")
}

func TestDownloadPluginInvalidComponentReferenceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "plugin")

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"invalid-component-reference",
		"--output", outputPath,
		"--skip-validation",
		"--config", cfgPath,
	})
	err := downloadCMD.ExecuteContext(t.Context())
	require.Error(t, err, "DownloadPlugin should fail for invalid component reference")

	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	assert.NoFileExists(t, pluginLocation, "plugin binary should not be downloaded for invalid component reference")
}

func TestDownloadPluginWithValidationFailureIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	user, password := getUserAndPasswordForTest(t)
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: %[1]q
        password: %[2]q
`, user, password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "ecrplugin")

	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download",
		"plugin",
		"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0",
		"--extra-identity", "os=linux",
		"--extra-identity", "architecture=amd64",
		"--output", outputPath,
		"--config", cfgPath,
	})
	err := downloadCMD.ExecuteContext(t.Context())

	// This should fail because ecrplugin is not a valid OCM plugin
	require.Error(t, err, "DownloadPlugin should fail when validation is enabled for ecrplugin")
	assert.Contains(t, err.Error(), "downloaded binary is not a valid plugin", "error should mention plugin validation failure")

	// The binary should be cleaned up after validation failure
	pluginLocation := filepath.Join(outputPath, "ecrplugin")
	assert.NoFileExists(t, pluginLocation, "plugin binary should be removed after validation failure")
}

// getUserAndPasswordForTest safely gets GitHub credentials for testing
func getUserAndPasswordForTest(t *testing.T) (string, string) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh CLI not found, skipping test")
	}

	user, err := getUsername(t, gh)
	if err != nil {
		t.Errorf("gh CLI for username failed: %v", err)
		return "", ""
	}

	pw := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s auth token", gh))
	out, err := pw.CombinedOutput()
	if err != nil {
		t.Logf("gh auth token output: %s", out)
		t.Errorf("gh CLI for password failed: %v", err)
	}
	password := strings.TrimSpace(string(out))

	return user, password
}

func getUsername(t *testing.T, gh string) (string, error) {
	if githubUser := os.Getenv("GITHUB_USER"); githubUser != "" {
		return githubUser, nil
	}

	out, err := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		t.Logf("gh CLI output: %s", out)
		return "", fmt.Errorf("gh CLI for user failed: %w", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		t.Logf("gh CLI output: %s", out)
		return "", fmt.Errorf("gh failed to parse output: %w", err)
	}

	return structured["login"].(string), nil
}
