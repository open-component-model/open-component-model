package log

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterLoggingFlags(t *testing.T) {
	cmd := &cobra.Command{}
	RegisterLoggingFlags(cmd)

	// Verify that all flags are registered
	assert.NotNil(t, cmd.PersistentFlags().Lookup(FormatFlagName))
	assert.NotNil(t, cmd.PersistentFlags().Lookup(LevelFlagName))
	assert.NotNil(t, cmd.PersistentFlags().Lookup(OutputFlagName))
}

func TestGetBaseLogger(t *testing.T) {
	tests := []struct {
		name   string
		format string
		level  string
		output string
	}{
		{
			name:   "valid json format with debug level to stdout",
			format: FormatJSON,
			level:  LevelDebug,
			output: OutputStdout,
		},
		{
			name:   "valid text format with info level to stderr",
			format: FormatText,
			level:  LevelInfo,
			output: OutputStderr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			RegisterLoggingFlags(cmd)

			// Set the flags
			require.NoError(t, cmd.PersistentFlags().Set(FormatFlagName, tt.format))
			require.NoError(t, cmd.PersistentFlags().Set(LevelFlagName, tt.level))
			require.NoError(t, cmd.PersistentFlags().Set(OutputFlagName, tt.output))

			// Create a buffer for output
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			logger, err := GetBaseLogger(cmd)
			assert.NoError(t, err)
			assert.NotNil(t, logger)
		})
	}
}

func TestLoggerLevelFromCommand(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		expectLevel slog.Level
	}{
		{
			name:        "debug level",
			level:       LevelDebug,
			expectLevel: slog.LevelDebug,
		},
		{
			name:        "info level",
			level:       LevelInfo,
			expectLevel: slog.LevelInfo,
		},
		{
			name:        "warn level",
			level:       LevelWarn,
			expectLevel: slog.LevelWarn,
		},
		{
			name:        "error level",
			level:       LevelError,
			expectLevel: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			RegisterLoggingFlags(cmd)
			require.NoError(t, cmd.PersistentFlags().Set(LevelFlagName, tt.level))

			level, err := loggerLevelFromCommand(cmd)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectLevel, level)
		})
	}
}
