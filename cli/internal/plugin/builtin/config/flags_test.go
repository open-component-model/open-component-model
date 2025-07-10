package config

import (
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"

	configv1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/flags/log"
	builtinv1 "ocm.software/open-component-model/cli/internal/plugin/builtin/config/v1"
)

func createTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	log.RegisterLoggingFlags(cmd.Flags())
	AddBuiltinPluginFlags(cmd.Flags())
	return cmd
}

func setFlag(cmd *cobra.Command, name, value string) {
	cmd.Flags().Set(name, value)
	cmd.Flags().Lookup(name).Changed = true
}

func createTestConfig(logLevel, logFormat, tempFolder string) *configv1.Config {
	builtinConfig := &builtinv1.BuiltinPluginConfig{
		Type:       runtime.NewVersionedType(builtinv1.ConfigType, builtinv1.ConfigTypeV1),
		LogLevel:   builtinv1.LogLevel(logLevel),
		LogFormat:  builtinv1.LogFormat(logFormat),
		TempFolder: tempFolder,
	}

	data, _ := json.Marshal(builtinConfig)

	return &configv1.Config{
		Type: runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{
			{
				Type: builtinConfig.GetType(),
				Data: data,
			},
		},
	}
}

func TestGetBuiltinPluginConfig_ConfigFileOnly(t *testing.T) {
	tests := []struct {
		name               string
		configLogLevel     string
		configLogFormat    string
		configTempFolder   string
		expectedLogLevel   builtinv1.LogLevel
		expectedLogFormat  builtinv1.LogFormat
		expectedTempFolder string
	}{
		{
			name:               "config file with debug level and json format",
			configLogLevel:     "debug",
			configLogFormat:    "json",
			configTempFolder:   "/tmp/custom",
			expectedLogLevel:   builtinv1.LogLevelDebug,
			expectedLogFormat:  builtinv1.LogFormatJSON,
			expectedTempFolder: "/tmp/custom",
		},
		{
			name:               "config file with partial settings",
			configLogLevel:     "warn",
			configLogFormat:    "",
			configTempFolder:   "",
			expectedLogLevel:   builtinv1.LogLevelWarn,
			expectedLogFormat:  builtinv1.LogFormatText, // default
			expectedTempFolder: "",
		},
		{
			name:               "empty config file uses defaults",
			configLogLevel:     "",
			configLogFormat:    "",
			configTempFolder:   "",
			expectedLogLevel:   builtinv1.LogLevelInfo,  // default
			expectedLogFormat:  builtinv1.LogFormatText, // default
			expectedTempFolder: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := createTestCommand()
			config := createTestConfig(tt.configLogLevel, tt.configLogFormat, tt.configTempFolder)

			result, err := GetBuiltinPluginConfig(cmd, config)
			if err != nil {
				t.Fatalf("GetBuiltinPluginConfig() error = %v", err)
			}

			if result.LogLevel != tt.expectedLogLevel {
				t.Errorf("LogLevel = %v, want %v", result.LogLevel, tt.expectedLogLevel)
			}
			if result.LogFormat != tt.expectedLogFormat {
				t.Errorf("LogFormat = %v, want %v", result.LogFormat, tt.expectedLogFormat)
			}
			if result.TempFolder != tt.expectedTempFolder {
				t.Errorf("TempFolder = %v, want %v", result.TempFolder, tt.expectedTempFolder)
			}
		})
	}
}

func TestGetBuiltinPluginConfig_CLIFlagsOnly(t *testing.T) {
	tests := []struct {
		name               string
		cliLogLevel        string
		cliLogFormat       string
		cliTempFolder      string
		expectedLogLevel   builtinv1.LogLevel
		expectedLogFormat  builtinv1.LogFormat
		expectedTempFolder string
	}{
		{
			name:               "CLI flags override defaults",
			cliLogLevel:        "error",
			cliLogFormat:       "json",
			cliTempFolder:      "/tmp/cli",
			expectedLogLevel:   builtinv1.LogLevelError,
			expectedLogFormat:  builtinv1.LogFormatJSON,
			expectedTempFolder: "/tmp/cli",
		},
		{
			name:               "partial CLI flags",
			cliLogLevel:        "debug",
			cliLogFormat:       "",
			cliTempFolder:      "",
			expectedLogLevel:   builtinv1.LogLevelDebug,
			expectedLogFormat:  builtinv1.LogFormatText, // default
			expectedTempFolder: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := createTestCommand()

			// Set CLI flags
			if tt.cliLogLevel != "" {
				setFlag(cmd, log.LevelFlagName, tt.cliLogLevel)
			}
			if tt.cliLogFormat != "" {
				setFlag(cmd, log.FormatFlagName, tt.cliLogFormat)
			}
			if tt.cliTempFolder != "" {
				setFlag(cmd, "temp-folder", tt.cliTempFolder)
			}

			// No config file
			result, err := GetBuiltinPluginConfig(cmd, nil)
			if err != nil {
				t.Fatalf("GetBuiltinPluginConfig() error = %v", err)
			}

			if result.LogLevel != tt.expectedLogLevel {
				t.Errorf("LogLevel = %v, want %v", result.LogLevel, tt.expectedLogLevel)
			}
			if result.LogFormat != tt.expectedLogFormat {
				t.Errorf("LogFormat = %v, want %v", result.LogFormat, tt.expectedLogFormat)
			}
			if result.TempFolder != tt.expectedTempFolder {
				t.Errorf("TempFolder = %v, want %v", result.TempFolder, tt.expectedTempFolder)
			}
		})
	}
}

func TestGetBuiltinPluginConfig_Precedence(t *testing.T) {
	tests := []struct {
		name               string
		configLogLevel     string
		configLogFormat    string
		configTempFolder   string
		cliLogLevel        string
		cliLogFormat       string
		cliTempFolder      string
		expectedLogLevel   builtinv1.LogLevel
		expectedLogFormat  builtinv1.LogFormat
		expectedTempFolder string
	}{
		{
			name:               "CLI flags override config file completely",
			configLogLevel:     "info",
			configLogFormat:    "text",
			configTempFolder:   "/tmp/config",
			cliLogLevel:        "debug",
			cliLogFormat:       "json",
			cliTempFolder:      "/tmp/cli",
			expectedLogLevel:   builtinv1.LogLevelDebug,
			expectedLogFormat:  builtinv1.LogFormatJSON,
			expectedTempFolder: "/tmp/cli",
		},
		{
			name:               "CLI flags override config file partially",
			configLogLevel:     "warn",
			configLogFormat:    "json",
			configTempFolder:   "/tmp/config",
			cliLogLevel:        "error",
			cliLogFormat:       "",                      // not set
			cliTempFolder:      "",                      // not set
			expectedLogLevel:   builtinv1.LogLevelError, // CLI override
			expectedLogFormat:  builtinv1.LogFormatJSON, // from config
			expectedTempFolder: "/tmp/config",           // from config
		},
		{
			name:               "config file provides fallback for unset CLI flags",
			configLogLevel:     "debug",
			configLogFormat:    "text",
			configTempFolder:   "/tmp/config",
			cliLogLevel:        "",                      // not set
			cliLogFormat:       "json",                  // CLI override
			cliTempFolder:      "",                      // not set
			expectedLogLevel:   builtinv1.LogLevelDebug, // from config
			expectedLogFormat:  builtinv1.LogFormatJSON, // CLI override
			expectedTempFolder: "/tmp/config",           // from config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := createTestCommand()
			config := createTestConfig(tt.configLogLevel, tt.configLogFormat, tt.configTempFolder)

			// Set CLI flags
			if tt.cliLogLevel != "" {
				setFlag(cmd, log.LevelFlagName, tt.cliLogLevel)
			}
			if tt.cliLogFormat != "" {
				setFlag(cmd, log.FormatFlagName, tt.cliLogFormat)
			}
			if tt.cliTempFolder != "" {
				setFlag(cmd, "temp-folder", tt.cliTempFolder)
			}

			result, err := GetBuiltinPluginConfig(cmd, config)
			if err != nil {
				t.Fatalf("GetBuiltinPluginConfig() error = %v", err)
			}

			if result.LogLevel != tt.expectedLogLevel {
				t.Errorf("LogLevel = %v, want %v", result.LogLevel, tt.expectedLogLevel)
			}
			if result.LogFormat != tt.expectedLogFormat {
				t.Errorf("LogFormat = %v, want %v", result.LogFormat, tt.expectedLogFormat)
			}
			if result.TempFolder != tt.expectedTempFolder {
				t.Errorf("TempFolder = %v, want %v", result.TempFolder, tt.expectedTempFolder)
			}
		})
	}
}

func TestGetMergedConfigWithCLIFlags_NoOverrides(t *testing.T) {
	cmd := createTestCommand()
	baseConfig := createTestConfig("info", "text", "/tmp/base")

	// No CLI flags set
	result, err := GetMergedConfigWithCLIFlags(cmd, baseConfig)
	if err != nil {
		t.Fatalf("GetMergedConfigWithCLIFlags() error = %v", err)
	}

	// Should return original config when no overrides
	if result != baseConfig {
		t.Errorf("Expected original config to be returned when no CLI overrides")
	}
}

func TestGetMergedConfigWithCLIFlags_WithOverrides(t *testing.T) {
	cmd := createTestCommand()
	baseConfig := createTestConfig("info", "text", "/tmp/base")

	// Set CLI flags
	setFlag(cmd, log.LevelFlagName, "debug")
	setFlag(cmd, log.FormatFlagName, "json")

	result, err := GetMergedConfigWithCLIFlags(cmd, baseConfig)
	if err != nil {
		t.Fatalf("GetMergedConfigWithCLIFlags() error = %v", err)
	}

	// Should return a different config with merged values
	if result == baseConfig {
		t.Errorf("Expected new merged config, got original config")
	}

	// Verify the merged config contains the CLI overrides
	builtinConfig, err := builtinv1.LookupBuiltinPluginConfig(result)
	if err != nil {
		t.Fatalf("Failed to lookup builtin config from merged config: %v", err)
	}

	if builtinConfig.LogLevel != builtinv1.LogLevelDebug {
		t.Errorf("Expected LogLevel to be debug, got %v", builtinConfig.LogLevel)
	}
	if builtinConfig.LogFormat != builtinv1.LogFormatJSON {
		t.Errorf("Expected LogFormat to be json, got %v", builtinConfig.LogFormat)
	}
	if builtinConfig.TempFolder != "/tmp/base" {
		t.Errorf("Expected TempFolder to remain /tmp/base, got %v", builtinConfig.TempFolder)
	}
}

func TestGetMergedConfigWithCLIFlags_NoBuiltinConfigInBase(t *testing.T) {
	cmd := createTestCommand()

	// Create config without builtin plugin configuration
	baseConfig := &configv1.Config{
		Type:           runtime.NewVersionedType(configv1.ConfigType, configv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{}, // Empty
	}

	// Set CLI flags
	setFlag(cmd, log.LevelFlagName, "error")
	setFlag(cmd, "temp-folder", "/tmp/new")

	result, err := GetMergedConfigWithCLIFlags(cmd, baseConfig)
	if err != nil {
		t.Fatalf("GetMergedConfigWithCLIFlags() error = %v", err)
	}

	// Should add new builtin config to merged config
	if len(result.Configurations) != 1 {
		t.Errorf("Expected 1 configuration in merged config, got %d", len(result.Configurations))
	}

	// Verify the added config contains the CLI values
	builtinConfig, err := builtinv1.LookupBuiltinPluginConfig(result)
	if err != nil {
		t.Fatalf("Failed to lookup builtin config from merged config: %v", err)
	}

	if builtinConfig.LogLevel != builtinv1.LogLevelError {
		t.Errorf("Expected LogLevel to be error, got %v", builtinConfig.LogLevel)
	}
	if builtinConfig.TempFolder != "/tmp/new" {
		t.Errorf("Expected TempFolder to be /tmp/new, got %v", builtinConfig.TempFolder)
	}
}

func TestGetMergedConfigWithCLIFlags_NilConfig(t *testing.T) {
	cmd := createTestCommand()

	result, err := GetMergedConfigWithCLIFlags(cmd, nil)
	if err != nil {
		t.Fatalf("GetMergedConfigWithCLIFlags() error = %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil result for nil input config")
	}
}
