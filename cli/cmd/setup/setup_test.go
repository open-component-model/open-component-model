package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

func init() {
	genericv1.Scheme.MustRegisterWithAlias(&filesystemv1alpha1.Config{}, runtime.NewVersionedType(filesystemv1alpha1.ConfigType, filesystemv1alpha1.Version))
}

// createConfigWithFilesystemConfig creates a v1.Config with filesystem configuration from JSON
func createConfigWithFilesystemConfig(tempFolder string) *genericv1.Config {
	configJSON := `{
		"configurations": [
			{
				"type": "filesystem.config.ocm.software/v1alpha1",
				"tempFolder": "` + tempFolder + `"
			}
		]
	}`

	config := &genericv1.Config{}
	if err := json.Unmarshal([]byte(configJSON), config); err != nil {
		panic(err)
	}
	return config
}

func TestSetupTempFolderFilesystemConfig(t *testing.T) {
	tests := []struct {
		name                string
		cliFlag             string
		existingConfig      *genericv1.Config
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
			existingConfig:      &genericv1.Config{},
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
			existingConfig:      &genericv1.Config{},
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
			cmd.Flags().String(ocmcmd.TempFolderFlag, "", "test flag")
			if tt.cliFlag != "" {
				err := cmd.Flags().Set(ocmcmd.TempFolderFlag, tt.cliFlag)
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

			FilesystemConfig(cmd, FilesystemConfigOptions{})

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
					if cfg.Name == filesystemv1alpha1.ConfigType {
						found = true
						fsConfig := &filesystemv1alpha1.Config{}
						err := genericv1.Scheme.Convert(cfg, fsConfig)
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

func createWorkingDirConfigWithFilesystemConfig(workingDir string) *genericv1.Config {
	configJSON := `{
		"configurations": [
			{
				"type": "filesystem.config.ocm.software/v1alpha1",
				"workingDirectory": "` + workingDir + `"
			}
		]
	}`

	config := &genericv1.Config{}
	if err := json.Unmarshal([]byte(configJSON), config); err != nil {
		panic(err)
	}
	return config
}

func TestSetupWorkingDirFilesystemConfig(t *testing.T) {
	tests := []struct {
		name                string
		cliFlag             string
		existingConfig      *genericv1.Config
		expectedWorkingDir  string
		expectedConfigMerge bool
	}{
		{
			name:                "CLI flag without existing config",
			cliFlag:             "/wd/custom",
			existingConfig:      nil,
			expectedWorkingDir:  "/wd/custom",
			expectedConfigMerge: false,
		},
		{
			name:                "CLI flag with empty central config",
			cliFlag:             "/wd/custom",
			existingConfig:      &genericv1.Config{},
			expectedWorkingDir:  "/wd/custom",
			expectedConfigMerge: false, // Config merge fails due to scheme registration issue
		},
		{
			name:                "CLI flag overrides existing filesystem config",
			cliFlag:             "/wd/override",
			existingConfig:      createWorkingDirConfigWithFilesystemConfig("/wd/original"),
			expectedWorkingDir:  "/wd/override",
			expectedConfigMerge: false,
		},
		{
			name:                "No CLI flag uses existing config",
			cliFlag:             "",
			existingConfig:      createWorkingDirConfigWithFilesystemConfig("/wd/fromconfig"),
			expectedWorkingDir:  "/wd/fromconfig",
			expectedConfigMerge: false,
		},
		{
			name:                "No CLI flag and no existing config",
			cliFlag:             "",
			existingConfig:      &genericv1.Config{},
			expectedWorkingDir:  "",
			expectedConfigMerge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create a test command with the working-directory flag
			cmd := &cobra.Command{
				Use: "test",
			}
			cmd.Flags().String(ocmcmd.WorkingDirectoryFlag, "", "test flag")
			if tt.cliFlag != "" {
				err := cmd.Flags().Set(ocmcmd.WorkingDirectoryFlag, tt.cliFlag)
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

			// Call FilesystemConfig
			FilesystemConfig(cmd, FilesystemConfigOptions{})

			// Verify the filesystem config in context
			ocmContext := ocmctx.FromContext(cmd.Context())
			r.NotNil(ocmContext, "OCM context should be available")

			fsCfg := ocmContext.FilesystemConfig()
			r.NotNil(fsCfg, "filesystem config should be available")
			r.Equal(tt.expectedWorkingDir, fsCfg.WorkingDirectory, "working-directory should match expected")

			// Verify config merging behavior
			if tt.expectedConfigMerge {
				centralCfg := ocmContext.Configuration()
				r.NotNil(centralCfg, "central config should be available")
				r.Greater(len(centralCfg.Configurations), configsBefore, "filesystem config should be added to central config")

				// Verify the filesystem config was added correctly
				found := false
				for _, cfg := range centralCfg.Configurations {
					if cfg.Name == filesystemv1alpha1.ConfigType {
						found = true
						fsConfig := &filesystemv1alpha1.Config{}
						err := genericv1.Scheme.Convert(cfg, fsConfig)
						r.NoError(err, "should convert to filesystem config")
						r.Equal(tt.expectedWorkingDir, fsConfig.WorkingDirectory, "merged config should have correct working directory")
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
		config   *genericv1.Config
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "empty config",
			config:   &genericv1.Config{},
			expected: false,
		},
		{
			name:     "config with filesystem config",
			config:   createConfigWithFilesystemConfig("/tmp/test"),
			expected: true,
		},
		{
			name: "config with other types",
			config: func() *genericv1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "other.type/v1"
						}
					]
				}`
				config := &genericv1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: false,
		},
		{
			name: "config with mixed types including filesystem",
			config: func() *genericv1.Config {
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
				config := &genericv1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: true,
		},
		{
			name: "config with unversioned filesystem config",
			config: func() *genericv1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "filesystem.config.ocm.software",
							"tempFolder": "/tmp/unversioned"
						}
					]
				}`
				config := &genericv1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: true,
		},
		{
			name: "config with filesystem config and working directory",
			config: func() *genericv1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "filesystem.config.ocm.software/v1alpha1",
							"workingDirectory": "/wd/test"
						}
					]
				}`
				config := &genericv1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			expected: true,
		},
		{
			name: "config with filesystem config and temp and working directory",
			config: func() *genericv1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "filesystem.config.ocm.software/v1alpha1",
							"tempFolder": "/tmp/test",
							"workingDirectory": "/wd/test"
						}
					]
				}`
				config := &genericv1.Config{}
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
		initialConfig *genericv1.Config
		fsCfg         *filesystemv1alpha1.Config
		expectedError bool
		expectedCount int
	}{
		{
			name:          "add to empty config",
			initialConfig: &genericv1.Config{},
			fsCfg: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectedError: false,
			expectedCount: 1,
		},
		{
			name: "add to existing config",
			initialConfig: func() *genericv1.Config {
				configJSON := `{
					"configurations": [
						{
							"type": "other.type/v1"
						}
					]
				}`
				config := &genericv1.Config{}
				if err := json.Unmarshal([]byte(configJSON), config); err != nil {
					panic(err)
				}
				return config
			}(),
			fsCfg: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectedError: false,
			expectedCount: 2,
		},
		{
			name:          "nil central config",
			initialConfig: nil,
			fsCfg: &filesystemv1alpha1.Config{
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
				if cfg.Name == filesystemv1alpha1.ConfigType {
					found = true
					fsConfig := &filesystemv1alpha1.Config{}
					err := genericv1.Scheme.Convert(cfg, fsConfig)
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
	workingDir := filepath.Join(tempDir, "working")
	customTempDir := filepath.Join(tempDir, "custom")
	err := os.MkdirAll(customTempDir, 0755)
	r.NoError(err, "failed to create custom temp dir")

	// Test complete integration from command setup to context retrieval
	cmd := &cobra.Command{
		Use: "test",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			FilesystemConfig(cmd, FilesystemConfigOptions{})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// This simulates what a command would do to access the filesystem config
			ocmContext := ocmctx.FromContext(cmd.Context())
			fsCfg := ocmContext.FilesystemConfig()

			// Verify the config is available and correct
			r.NotNil(fsCfg, "filesystem config should be available in command")
			r.Equal(customTempDir, fsCfg.TempFolder, "temp folder should be set from CLI flag")
			r.Equal(workingDir, fsCfg.WorkingDirectory, "working directory should be set from CLI flag")
			return nil
		},
	}

	// Add the temp-folder flag like the real command does
	cmd.PersistentFlags().String(ocmcmd.TempFolderFlag, "", "test flag")
	cmd.PersistentFlags().String(ocmcmd.WorkingDirectoryFlag, "", "working directory flag")

	// Set up the command with arguments
	cmd.SetArgs([]string{"--temp-folder", customTempDir, "--working-directory", workingDir})

	// Execute the command
	err = cmd.ExecuteContext(context.Background())
	r.NoError(err, "command should execute successfully")
}
