package hooks

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/cli/cmd/global"
	"ocm.software/open-component-model/cli/cmd/setup"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

func loadFlagFromCommand(cmd *cobra.Command, flagName string) (string, error) {
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

type PreRunOptions any
type FilesystemOptions func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error

func WithWorkingDirectory(value string) FilesystemOptions {
	return func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error {
		fsCfgOptions[global.WorkingDirectoryFlag] = setup.WithWorkingDirectory(value)
		return nil
	}
}

func WithTempFolder(value string) FilesystemOptions {
	return func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error {
		fsCfgOptions[global.TempFolderFlag] = setup.WithTempFolder(value)
		return nil
	}
}

// PreRunE sets up the Cmd command with the necessary setup for all cli commands.
func PreRunE(cmd *cobra.Command, _ []string) error {
	return PreRunEWithOptions(cmd, nil)
}

// PreRunEWithOptions sets up the Cmd command with the necessary setup for all cli commands.
// It allows passing additional options to customize the setup process.
func PreRunEWithOptions(cmd *cobra.Command, _ []string, opts ...PreRunOptions) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}
	slog.SetDefault(logger)

	setup.SetupOCMConfig(cmd)

	// CLI flag takes precedence over the config file
	fsCfgOptionsMap := make(map[string]setup.SetupFilesystemConfigOption)
	tempFolderValue, _ := loadFlagFromCommand(cmd, global.TempFolderFlag)
	workingDirectoryValue, _ := loadFlagFromCommand(cmd, global.WorkingDirectoryFlag)

	// Initialize filesystem configuration options
	for _, opt := range opts {
		if filesystemOpt, ok := opt.(FilesystemOptions); ok {
			if err := filesystemOpt(cmd, fsCfgOptionsMap); err != nil {
				return fmt.Errorf("could not apply filesystem option: %w", err)
			}
		}
	}

	// cli flags take precedence over the config file
	if tempFolderValue != "" {
		fsCfgOptionsMap[global.TempFolderFlag] = setup.WithTempFolder(tempFolderValue)
	}
	if workingDirectoryValue != "" {
		fsCfgOptionsMap[global.WorkingDirectoryFlag] = setup.WithWorkingDirectory(workingDirectoryValue)
	}

	// fsCfgOptionsMap to slice
	fsCfgOption := make([]setup.SetupFilesystemConfigOption, 0, len(fsCfgOptionsMap))
	for _, opt := range fsCfgOptionsMap {
		fsCfgOption = append(fsCfgOption, opt)
	}

	setup.SetupFilesystemConfig(cmd, fsCfgOption...)

	if err := setup.SetupPluginManager(cmd); err != nil {
		return fmt.Errorf("could not setup plugin manager: %w", err)
	}

	if err := setup.SetupCredentialGraph(cmd); err != nil {
		return fmt.Errorf("could not setup credential graph: %w", err)
	}

	ocmctx.Register(cmd)

	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	return nil
}
