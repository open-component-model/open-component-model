package v1

import (
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType   = "builtin.plugin.config.ocm.software"
	ConfigTypeV1 = Version
)

// LogFormat defines the supported log formats for built-in plugins.
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// LogLevel defines the supported log levels for built-in plugins.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// BuiltinPluginConfig holds configuration for built-in plugins.
// This configuration is passed directly to built-in plugins as a typed struct,
// unlike external plugins which receive runtime.Raw configuration.
// LogLevel and LogFormat can be set via config file and overridden by CLI flags.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type BuiltinPluginConfig struct {
	Type runtime.Type `json:"type"`

	// Logger configuration (can be overridden by CLI flags)
	LogLevel  LogLevel  `json:"logLevel,omitempty"`
	LogFormat LogFormat `json:"logFormat,omitempty"`

	// TempFolder specifies the temporary folder location for plugin operations.
	// If empty, the system default temp directory is used.
	TempFolder string `json:"tempFolder,omitempty"`
}

// GetLogLevel returns the slog.Level equivalent of the configured log level.
func (c *BuiltinPluginConfig) GetLogLevel() slog.Level {
	switch c.LogLevel {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// GetTempFolder returns the configured temp folder or system default.
func (c *BuiltinPluginConfig) GetTempFolder() string {
	if c.TempFolder != "" {
		return c.TempFolder
	}
	return os.TempDir()
}

// DefaultBuiltinPluginConfig returns a default configuration for built-in plugins.
func DefaultBuiltinPluginConfig() *BuiltinPluginConfig {
	return &BuiltinPluginConfig{
		Type:      runtime.NewVersionedType(ConfigType, ConfigTypeV1),
		LogLevel:  LogLevelInfo,
		LogFormat: LogFormatText,
	}
}
