// Package log provides logging functionality for the Open Component Model CLI.
// It supports different log formats (JSON, text), log levels (debug, info, warn, error),
// and output destinations (stdout, stderr).
package log

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ocm.software/open-component-model/cli/internal/flags/enum"
)

// Log format constants
const (
	FormatFlagName = "logformat" // Flag name for log format configuration

	FormatJSON = "json" // JSON format for structured logging, suitable for machine processing
	FormatText = "text" // Human-readable text format, suitable for console output
)

// Log level constants
const (
	LevelFlagName = "loglevel" // Flag name for log level configuration

	LevelDebug = "debug" // Debug level for detailed debugging information, including internal state
	LevelInfo  = "info"  // Info level for general operational information about what's happening
	LevelWarn  = "warn"  // Warn level for warning conditions that don't prevent operation
	LevelError = "error" // Error level for error conditions that affect operation
)

// Log output constants
const (
	OutputFlagName = "logoutput" // Flag name for log output configuration

	OutputStdout = "stdout" // Standard output destination, suitable for normal operation
	OutputStderr = "stderr" // Standard error destination, suitable for error conditions
)

// RegisterLoggingFlags registers the logging-related flags with the provided cobra command.
// It adds flags for FormatFlagName, LevelFlagName, and OutputFlagName.
// The flags are added as persistent flags, meaning they will be available to the command
// and all its subcommands.
//
// Usage examples:
//
//	--logformat json     # Output logs in JSON format for machine processing
//	--logformat text     # Output logs in human-readable text format
//	--loglevel debug     # Show all logs including debug information
//	--loglevel info      # Show informational messages and above
//	--loglevel warn      # Show warnings and errors only (default)
//	--loglevel error     # Show errors only
//	--logoutput stdout   # Write logs to standard output
//	--logoutput stderr   # Write logs to standard error
//
// These flags can be combined, for example:
//
//	--logformat json --loglevel debug --logoutput stderr
func RegisterLoggingFlags(flagset *pflag.FlagSet) {
	enum.Var(flagset, FormatFlagName, []string{
		FormatText,
		FormatJSON,
	}, `set the log output format that is used to print individual logs
   json: Output logs in JSON format, suitable for machine processing
   text: Output logs in human-readable text format, suitable for console output`)

	enum.Var(flagset, LevelFlagName, []string{
		LevelInfo,
		LevelDebug,
		LevelWarn,
		LevelError,
	}, `sets the logging level
   debug: Show all logs including detailed debugging information
   info:  Show informational messages and above
   warn:  Show warnings and errors only (default)
   error: Show errors only`)

	enum.Var(flagset, OutputFlagName, []string{
		OutputStdout,
		OutputStderr,
	}, `set the log output destination
   stdout: Write logs to standard output (default)
   stderr: Write logs to standard error, useful for separating logs from normal output`)
}

// GetBaseLogger creates and returns a new slog.Logger instance based on the command's flags.
// It configures the logger with the specified log level, format, and output destination.
// Returns an error if any of the flag values are invalid or if there's an issue
// retrieving the flag values.
func GetBaseLogger(cmd *cobra.Command) (*slog.Logger, error) {
	logLevel, err := loggerLevelFromCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to get log level: %w", err)
	}

	format, err := enum.Get(cmd.Flags(), FormatFlagName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the log format from the command flag: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), OutputFlagName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the log output from the command flag: %w", err)
	}

	var outputWriter io.Writer
	switch output {
	case OutputStdout:
		outputWriter = cmd.OutOrStdout()
	case OutputStderr:
		outputWriter = cmd.OutOrStderr()
	}

	var handler slog.Handler
	switch format {
	case FormatJSON:
		handler = slog.NewJSONHandler(outputWriter, &slog.HandlerOptions{
			Level: logLevel,
		})
	case FormatText:
		handler = slog.NewTextHandler(outputWriter, &slog.HandlerOptions{
			Level: logLevel,
		})
	default:
		return nil, fmt.Errorf("invalid log format: %s", format)
	}

	return slog.New(handler), nil
}

// loggerLevelFromCommand converts the log level string from the command flags
// to the corresponding slog.Level value. Returns an error if the log level
// string is invalid.
func loggerLevelFromCommand(cmd *cobra.Command) (slog.Level, error) {
	logLevel, err := enum.Get(cmd.Flags(), LevelFlagName)
	if err != nil {
		return slog.LevelWarn, err
	}
	var level slog.Level
	switch logLevel {
	case LevelDebug:
		level = slog.LevelDebug
	case LevelInfo:
		level = slog.LevelInfo
	case LevelWarn:
		level = slog.LevelWarn
	case LevelError:
		level = slog.LevelError
	default:
		return slog.LevelWarn, fmt.Errorf("invalid log level: %s", logLevel)
	}
	return level, nil
}
