package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/add"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/cmd/download"
	"ocm.software/open-component-model/cli/cmd/generate"
	"ocm.software/open-component-model/cli/cmd/get"
	"ocm.software/open-component-model/cli/cmd/version"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

const (
	tempFolderFlag               = "temp-folder"
	pluginShutdownTimeoutFlag    = "plugin-shutdown-timeout"
	pluginShutdownTimeoutDefault = 10 * time.Second
	pluginDirectoryFlag          = "plugin-directory"
)

var pluginDirectoryDefault = filepath.Join("$HOME", ".config", "ocm", "plugins")

// Execute adds all child commands to the Cmd command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the Cmd.
func Execute() {
	err := New().Execute()
	if err != nil {
		os.Exit(1)
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ocm [sub-command]",
		Short: "The official Open Component Model (OCM) CLI",
		Long: `The Open Component Model command line client supports the work with OCM
  artifacts, like Component Archives, Common Transport Archive,
  Component Repositories, and Component Versions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: preRunE,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	configuration.RegisterConfigFlag(cmd)
	cmd.PersistentFlags().String(tempFolderFlag, "", `Specify a custom temporary folder path for filesystem operations.`)
	cmd.PersistentFlags().Duration(pluginShutdownTimeoutFlag, pluginShutdownTimeoutDefault,
		`Timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed`)
	cmd.PersistentFlags().String(pluginDirectoryFlag, pluginDirectoryDefault, `default directory path for ocm plugins.`)
	log.RegisterLoggingFlags(cmd.PersistentFlags())
	cmd.AddCommand(generate.New())
	cmd.AddCommand(get.New())
	cmd.AddCommand(add.New())
	cmd.AddCommand(version.New())
	cmd.AddCommand(download.New())
	return cmd
}

// preRunE sets up the Cmd command with the necessary setup for all cli commands.
func preRunE(cmd *cobra.Command, _ []string) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}
	slog.SetDefault(logger)

	setupOCMConfig(cmd)
	setupFilesystemConfig(cmd)

	if err := setupPluginManager(cmd); err != nil {
		return fmt.Errorf("could not setup plugin manager: %w", err)
	}

	if err := setupCredentialGraph(cmd); err != nil {
		return fmt.Errorf("could not setup credential graph: %w", err)
	}

	ocmctx.Register(cmd)

	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	return nil
}
