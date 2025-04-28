package log

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/internal/enum"
)

func RegisterLoggingFlags(cmd *cobra.Command) {
	enum.Var(cmd.PersistentFlags(), "loglevel", []string{
		"warn",
		"debug",
		"info",
		"error",
	}, "set the log level (debug, info, warn, error, fatal)")
	cmd.PersistentFlags().StringP("logformat", "f", "text", "set the log format (text, json)")
}

func GetBaseLogger(cmd *cobra.Command) (*slog.Logger, error) {
	logLevel, err := GetLoggerLevel(cmd)
	if err != nil {
		return nil, err
	}

	format := cmd.Flag("logformat").Value.String()
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(cmd.OutOrStdout(), &slog.HandlerOptions{
			Level: logLevel,
		})
	case "text":
		handler = slog.NewTextHandler(cmd.OutOrStdout(), &slog.HandlerOptions{
			Level: logLevel,
		})
	default:
		return nil, fmt.Errorf("invalid log format: %s", format)
	}

	return slog.New(handler), nil
}

func GetLoggerLevel(cmd *cobra.Command) (slog.Level, error) {
	logLevel, err := enum.Get(cmd.Flags(), "loglevel")
	if err != nil {
		return slog.LevelWarn, err
	}
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return slog.LevelWarn, fmt.Errorf("invalid log level: %s", logLevel)
	}
	return level, nil
}
