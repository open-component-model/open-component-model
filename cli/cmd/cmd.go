package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/cli/cmd/sign"

	"ocm.software/open-component-model/cli/cmd/add"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/cmd/download"
	"ocm.software/open-component-model/cli/cmd/generate"
	"ocm.software/open-component-model/cli/cmd/get"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	"ocm.software/open-component-model/cli/cmd/setup/hooks"
	"ocm.software/open-component-model/cli/cmd/verify"
	"ocm.software/open-component-model/cli/cmd/version"
	"ocm.software/open-component-model/cli/internal/flags/log"
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
		PersistentPreRunE: hooks.PreRunE,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	configuration.RegisterConfigFlag(cmd)

	cmd.PersistentFlags().String(ocmcmd.TempFolderFlag, "", `Specify a custom temporary folder path for filesystem operations.`)
	cmd.PersistentFlags().Duration(ocmcmd.PluginShutdownTimeoutFlag, ocmcmd.PluginShutdownTimeoutDefault,
		`Timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed`)
	cmd.PersistentFlags().String(ocmcmd.PluginDirectoryFlag, pluginDirectoryDefault, `default directory path for ocm plugins.`)
	cmd.PersistentFlags().String(ocmcmd.WorkingDirectoryFlag, "", `Specify a custom working directory path to load resources from.`)
	log.RegisterLoggingFlags(cmd.PersistentFlags())
	cmd.AddCommand(generate.New())
	cmd.AddCommand(get.New())
	cmd.AddCommand(add.New())
	cmd.AddCommand(version.New())
	cmd.AddCommand(download.New())
	cmd.AddCommand(verify.New())
	cmd.AddCommand(sign.New())
	return cmd
}
