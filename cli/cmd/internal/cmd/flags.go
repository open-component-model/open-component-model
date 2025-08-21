package cmd

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"
)

const (
	TempFolderFlag               = "temp-folder"
	WorkingDirectoryFlag         = "working-directory"
	PluginShutdownTimeoutFlag    = "plugin-shutdown-timeout"
	PluginShutdownTimeoutDefault = 10 * time.Second
	PluginDirectoryFlag          = "plugin-directory"
)

func LoadFlagFromCommand(cmd *cobra.Command, flagName string) (string, error) {
	var (
		value string
		err   error
	)
	if flag := cmd.Flags().Lookup(flagName); flag != nil && flag.Changed {
		value, err = cmd.Flags().GetString(flagName)
		if err != nil {
			slog.DebugContext(cmd.Context(), "could not read flag value",
				slog.String("flag", flagName),
				slog.String("error", err.Error()))
		}
	}

	return value, err
}
