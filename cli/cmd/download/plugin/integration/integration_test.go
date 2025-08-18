package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	plugincmd "ocm.software/open-component-model/cli/cmd/download/plugin"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

// setupTestCommand creates a properly configured cobra command for testing
func setupTestCommand(t *testing.T, resourceName, resourceVersion, output string, extraIdentity []string, skipValidation bool) (*cobra.Command, context.Context) {
	t.Helper()
	cmd := &cobra.Command{
		Use: "test-download-plugin",
	}

	// Add required flags for plugin download
	cmd.Flags().String("resource-name", resourceName, "resource name")
	cmd.Flags().String("resource-version", resourceVersion, "resource version")
	cmd.Flags().String("output", output, "output path")
	cmd.Flags().StringSlice("extra-identity", extraIdentity, "extra identity")
	cmd.Flags().Bool("skip-validation", skipValidation, "skip validation")

	// Add logging flags using the correct enum flags
	log.RegisterLoggingFlags(cmd.Flags())

	// Set default values for logging flags
	_ = cmd.Flags().Set("loglevel", "warn")
	_ = cmd.Flags().Set("logformat", "text")
	_ = cmd.Flags().Set("logoutput", "stdout")

	// Set up context with plugin manager and credential graph
	ctx := context.Background()

	// Create plugin manager
	pluginManager := manager.NewPluginManager(ctx)

	// Create filesystem config for built-in plugins
	filesystemConfig := &filesystemv1alpha1.Config{}

	// Register built-in plugins
	if err := builtin.Register(pluginManager, filesystemConfig); err != nil {
		panic("failed to register builtin plugins: " + err.Error())
	}

	// Create credential graph using proper initialization like in setup.go
	opts := credentials.Options{
		RepositoryPluginProvider: pluginManager.CredentialRepositoryRegistry,
		CredentialPluginProvider: credentials.GetCredentialPluginFn(
			func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
				return nil, fmt.Errorf("no credential plugin found for type %s", typed)
			},
		),
		CredentialRepositoryTypeScheme: pluginManager.CredentialRepositoryRegistry.RepositoryScheme(),
	}

	user, password := getUserAndPasswordForTest(t)
	credCfg := &credentialsRuntime.Config{
		Repositories: []credentialsRuntime.RepositoryConfigEntry{
			{
				Repository: &runtime.Raw{
					Type: runtime.Type{
						Name:    "DockerConfig",
						Version: "v1",
					},
					Data: []byte(fmt.Sprintf(`{
							"auths": {
								"ghcr.io": {
									"username": "%s",
									"password": "%s"
								}
							}
						}`, user, password)),
				},
			},
		},
	}

	credentialGraph, err := credentials.ToGraph(ctx, credCfg, opts)
	if err != nil {
		panic("failed to create credential graph: " + err.Error())
	}

	// Set up context
	ctx = ocmctx.WithPluginManager(ctx, pluginManager)
	ctx = ocmctx.WithCredentialGraph(ctx, credentialGraph)
	ctx = ocmctx.WithFilesystemConfig(ctx, filesystemConfig)
	cmd.SetContext(ctx)

	return cmd, ctx
}

func TestDownloadPluginIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "ecrplugin")

	cmd, _ := setupTestCommand(t, "demo", "", outputPath, []string{"os=linux", "architecture=amd64"}, true)
	args := []string{"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0"}

	err := plugincmd.DownloadPlugin(cmd, args)
	require.NoError(t, err, "DownloadPlugin should succeed")
	assert.FileExists(t, outputPath, "binary should be downloaded")

	info, err := os.Stat(outputPath)
	require.NoError(t, err, "should be able to stat the downloaded file")
	assert.True(t, info.Mode().IsRegular(), "downloaded file should be a regular file")
	assert.Greater(t, info.Size(), int64(0), "downloaded file should not be empty")
}

func TestDownloadPluginMissingResourceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "nonexistent")
	cmd, _ := setupTestCommand(t, "nonexistent-resource", "", outputPath, []string{"os=linux", "architecture=amd64"}, true)
	args := []string{"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0"}
	err := plugincmd.DownloadPlugin(cmd, args)
	require.Error(t, err, "DownloadPlugin should fail for non-existent resource")
	assert.Contains(t, err.Error(), "no resource found matching identity", "error should mention missing resource")
	assert.NoFileExists(t, outputPath, "plugin binary should not be downloaded for non-existent resource")
}

func TestDownloadPluginInvalidComponentReferenceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "plugin")
	cmd, _ := setupTestCommand(t, "demo", "", outputPath, []string{}, true)
	args := []string{"invalid-component-reference"}
	err := plugincmd.DownloadPlugin(cmd, args)
	require.Error(t, err, "DownloadPlugin should fail for invalid component reference")
	assert.NoFileExists(t, outputPath, "plugin binary should not be downloaded for invalid component reference")
}

func TestDownloadPluginWithValidationFailureIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "ecrplugin")

	// Use the same ecrplugin sample but with validation enabled (skipValidation = false)
	cmd, _ := setupTestCommand(t, "demo", "", outputPath, []string{"os=linux", "architecture=amd64"}, false)
	args := []string{"ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0"}

	err := plugincmd.DownloadPlugin(cmd, args)

	// This should fail because ecrplugin is not a valid OCM plugin
	require.Error(t, err, "DownloadPlugin should fail when validation is enabled for ecrplugin")
	assert.Contains(t, err.Error(), "downloaded binary is not a valid plugin", "error should mention plugin validation failure")

	// The binary should be cleaned up after validation failure
	assert.NoFileExists(t, outputPath, "plugin binary should be removed after validation failure")
}

// getUserAndPasswordForTest safely gets GitHub credentials for testing
func getUserAndPasswordForTest(t *testing.T) (string, string) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh CLI not found, skipping test")
	}

	out, err := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		t.Logf("output from gh CLI: %s", out)
		t.Errorf("gh CLI for user failed: %v", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		t.Errorf("gh marshal failed: %v", err)
	}
	user := structured["login"].(string)

	pw := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s auth token", gh))
	if out, err = pw.CombinedOutput(); err != nil {
		t.Logf("output from sh auth token: %s", out)
		t.Errorf("gh CLI for password failed: %v", err)
	}
	password := strings.TrimSpace(string(out))

	return user, password
}
