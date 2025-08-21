package hooks

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"

	"ocm.software/open-component-model/cli/cmd/setup"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

type PreRunOptions any

type FilesystemOptions func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error

func WithWorkingDirectory(value string) FilesystemOptions {
	return func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error {
		fsCfgOptions[ocmcmd.WorkingDirectoryFlag] = setup.WithWorkingDirectory(value)
		return nil
	}
}

func WithTempFolder(value string) FilesystemOptions {
	return func(cmd *cobra.Command, fsCfgOptions map[string]setup.SetupFilesystemConfigOption) error {
		fsCfgOptions[ocmcmd.TempFolderFlag] = setup.WithTempFolder(value)
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
	tempFolderValue, _ := ocmcmd.LoadFlagFromCommand(cmd, ocmcmd.TempFolderFlag)
	workingDirectoryValue, _ := ocmcmd.LoadFlagFromCommand(cmd, ocmcmd.WorkingDirectoryFlag)

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
		fsCfgOptionsMap[ocmcmd.TempFolderFlag] = setup.WithTempFolder(tempFolderValue)
	}
	if workingDirectoryValue != "" {
		fsCfgOptionsMap[ocmcmd.WorkingDirectoryFlag] = setup.WithWorkingDirectory(workingDirectoryValue)
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
