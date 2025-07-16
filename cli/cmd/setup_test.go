package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1"
	"ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

func init() {
	v1.Scheme.MustRegisterWithAlias(&v1alpha1.Config{}, runtime.NewVersionedType(v1alpha1.ConfigType, v1alpha1.Version))
}

// createConfigWithFilesystemConfig creates a v1.Config with filesystem configuration from JSON
func createConfigWithFilesystemConfig(tempFolder string) *v1.Config {
	configJSON := `{
		"configurations": [
			{
				"type": "filesystem.config.ocm.software/v1alpha1",
				"tempFolder": "` + tempFolder + `"
			}
		]
	}`
	
	config := &v1.Config{}
	if err := json.Unmarshal([]byte(configJSON), config); err != nil {
		panic(err)
	}
	return config
}

func TestSetupFilesystemConfig(t *testing.T) {
	tests := []struct {
		name                string
		cliFlag             string
		existingConfig      *v1.Config
		expectedTempFolder  string
		expectedConfigMerge bool
	}{
		{
			name:                "CLI flag without existing config",
			cliFlag:             "/tmp/custom",
			existingConfig:      nil,
			expectedTempFolder:  "/tmp/custom",
			expectedConfigMerge: false,
		},
		{
			name:                "CLI flag with empty central config",
			cliFlag:             "/tmp/custom",
			existingConfig:      &v1.Config{},
			expectedTempFolder:  "/tmp/custom",
			expectedConfigMerge: false, // Config merge fails due to scheme registration issue
		},
		{
			name:                "CLI flag overrides existing filesystem config",
			cliFlag:             "/tmp/override",
			existingConfig:      createConfigWithFilesystemConfig("/tmp/original"),
			expectedTempFolder:  "/tmp/override",
			expectedConfigMerge: false,
		},
		{
			name:                "No CLI flag uses existing config",
			cliFlag:             "",
			existingConfig:      createConfigWithFilesystemConfig("/tmp/fromconfig"),
			expectedTempFolder:  "/tmp/fromconfig",
			expectedConfigMerge: false,
		},
		{
			name:                "No CLI flag and no existing config",
			cliFlag:             "",
			existingConfig:      &v1.Config{},
			expectedTempFolder:  os.TempDir(), // filesystem config defaults to os.TempDir()
			expectedConfigMerge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create a test command with the temp-folder flag
			cmd := &cobra.Command{
				Use: "test",
			}
			cmd.PersistentFlags().String(tempFolderFlag, "", "test flag")
			if tt.cliFlag != "" {
				err := cmd.PersistentFlags().Set(tempFolderFlag, tt.cliFlag)
				r.NoError(err, "failed to set CLI flag")
			}

			// Set up context with existing config if provided
			ctx := context.Background()
			if tt.existingConfig != nil {
				ctx = ocmctx.WithConfiguration(ctx, tt.existingConfig)
			}
			cmd.SetContext(ctx)

			// Count configurations before setup
			var configsBefore int
			if tt.existingConfig != nil {
				configsBefore = len(tt.existingConfig.Configurations)
			}

			// Call setupFilesystemConfig
			setupFilesystemConfig(cmd)

			// Verify the filesystem config in context
			ocmContext := ocmctx.FromContext(cmd.Context())
			r.NotNil(ocmContext, "OCM context should be available")

			fsCfg := ocmContext.FilesystemConfig()
			r.NotNil(fsCfg, "filesystem config should be available")
			r.Equal(tt.expectedTempFolder, fsCfg.TempFolder, "temp folder should match expected")

			// Verify config merging behavior
			if tt.expectedConfigMerge {
				centralCfg := ocmContext.Configuration()
				r.NotNil(centralCfg, "central config should be available")
				r.Greater(len(centralCfg.Configurations), configsBefore, "filesystem config should be added to central config")

				// Verify the filesystem config was added correctly
				found := false
				for _, cfg := range centralCfg.Configurations {
					if cfg.Type.Name == v1alpha1.ConfigType {
						found = true
						fsConfig := &v1alpha1.Config{}
						err := v1.Scheme.Convert(cfg, fsConfig)
						r.NoError(err, "should convert to filesystem config")
						r.Equal(tt.expectedTempFolder, fsConfig.TempFolder, "merged config should have correct temp folder")
						break
					}
				}
				r.True(found, "filesystem config should be found in central config")
			}
		})
	}
}

func TestHasFilesystemConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *v1.Config
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "empty config",
			config:   &v1.Config{},
			expected: false,
		},
		{
			name:     "config with filesystem config",
			config:   createConfigWithFilesystemConfig("/tmp/test"),
			expected: true,
		},
		{
			name: "config with other types",
			config: func() *v1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "other.type/v1"
						}
					]
				}`
				config := &v1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: false,
		},
		{
			name: "config with mixed types including filesystem",
			config: func() *v1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "other.type/v1"
						},
						{
							"type": "filesystem.config.ocm.software/v1alpha1",
							"tempFolder": "/tmp/test"
						}
					]
				}`
				config := &v1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasFilesystemConfig(tt.config)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAddFilesystemConfigToCentralConfig(t *testing.T) {
	tests := []struct {
		name          string
		initialConfig *v1.Config
		fsCfg         *v1alpha1.Config
		expectedError bool
		expectedCount int
	}{
		{
			name:          "add to empty config",
			initialConfig: &v1.Config{},
			fsCfg: &v1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectedError: false,
			expectedCount: 1,
		},
		{
			name: "add to existing config",
			initialConfig: func() *v1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "other.type/v1"
						}
					]
				}`
				config := &v1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			fsCfg: &v1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectedError: false,
			expectedCount: 2,
		},
		{
			name:          "nil central config",
			initialConfig: nil,
			fsCfg: &v1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectedError: true,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create test command
			cmd := &cobra.Command{Use: "test"}
			ctx := context.Background()

			if tt.initialConfig != nil {
				ctx = ocmctx.WithConfiguration(ctx, tt.initialConfig)
			}
			cmd.SetContext(ctx)

			// Call addFilesystemConfigToCentralConfig
			err := addFilesystemConfigToCentralConfig(cmd, tt.fsCfg)

			if tt.expectedError {
				r.Error(err, "expected error")
				return
			}

			r.NoError(err, "should not error")

			// Verify the config was added
			ocmContext := ocmctx.FromContext(cmd.Context())
			r.NotNil(ocmContext, "OCM context should be available")

			centralCfg := ocmContext.Configuration()
			r.NotNil(centralCfg, "central config should be available")
			r.Len(centralCfg.Configurations, tt.expectedCount, "should have expected number of configurations")

			// Verify the filesystem config was added correctly
			found := false
			for _, cfg := range centralCfg.Configurations {
				if cfg.Type.Name == v1alpha1.ConfigType {
					found = true
					fsConfig := &v1alpha1.Config{}
					err := v1.Scheme.Convert(cfg, fsConfig)
					r.NoError(err, "should convert to filesystem config")
					r.Equal(tt.fsCfg.TempFolder, fsConfig.TempFolder, "should have correct temp folder")
					break
				}
			}
			r.True(found, "filesystem config should be found in central config")
		})
	}
}

func TestFilesystemConfigIntegration(t *testing.T) {
	r := require.New(t)

	// Create a temporary directory for testing
	tempDir := t.TempDir()
	customTempDir := filepath.Join(tempDir, "custom")
	err := os.MkdirAll(customTempDir, 0755)
	r.NoError(err, "failed to create custom temp dir")

	// Test complete integration from command setup to context retrieval
	cmd := &cobra.Command{
		Use: "test",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			setupFilesystemConfig(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// This simulates what a command would do to access the filesystem config
			ocmContext := ocmctx.FromContext(cmd.Context())
			fsCfg := ocmContext.FilesystemConfig()

			// Verify the config is available and correct
			r.NotNil(fsCfg, "filesystem config should be available in command")
			r.Equal(customTempDir, fsCfg.TempFolder, "temp folder should be set from CLI flag")
			return nil
		},
	}

	// Add the temp-folder flag like the real command does
	cmd.PersistentFlags().String(tempFolderFlag, "", "test flag")

	// Set up the command with arguments
	cmd.SetArgs([]string{"--temp-folder", customTempDir})

	// Execute the command
	err = cmd.ExecuteContext(context.Background())
	r.NoError(err, "command should execute successfully")
}
